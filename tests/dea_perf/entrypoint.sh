#!/bin/bash
# Entrypoint for the godiam DEA performance test.
#
# Runs the same benchmark twice against the same backend: once through a plain
# relay agent and once through an edge agent with every dea_* control enabled.
# The delta between the two runs is the cost of the edge controls.

set -u

GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

BENCH_CONNECTIONS=${BENCH_CONNECTIONS:-10}
BENCH_DURATION=${BENCH_DURATION:-10s}
BENCH_RATE=${BENCH_RATE:-1000}
BENCH_WARMUP=${BENCH_WARMUP:-3s}

PARTNER_REALM="perf.partner.net"
FAILURES=0

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam DEA Performance Test${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""
echo "  Connections: $BENCH_CONNECTIONS"
echo "  Duration:    $BENCH_DURATION (after a $BENCH_WARMUP warmup)"
echo "  Target rate: $BENCH_RATE req/s"
echo ""

start_node() {
    local name=$1 conf=$2 log=$3
    /usr/local/bin/diameterd --config "$conf" > "$log" 2>&1 &
    local pid=$!
    sleep 2
    if ! kill -0 "$pid" 2>/dev/null; then
        echo -e "${RED}$name failed to start. Log:${NC}"
        cat "$log"
        return 1
    fi
    echo -e "${GREEN}  $name started (PID $pid)${NC}"
    echo "$pid" >> /tmp/pids
    return 0
}

: > /tmp/pids
cleanup() {
    while read -r pid; do kill "$pid" 2>/dev/null || true; done < /tmp/pids
}
trap cleanup EXIT

echo -e "${BLUE}[1/3] Starting nodes${NC}"
start_node "backend server" /conf/server.yaml /tmp/server.log || exit 1
start_node "baseline relay" /conf/baseline.yaml /tmp/baseline.log || exit 1
start_node "edge agent" /conf/dea.yaml /tmp/dea.log || exit 1

echo -e "${BLUE}  Waiting for agents to connect to the backend...${NC}"
sleep 3

# run_bench <label> <port> <identity> <extra bench args...>
# Prints the benchmark output to a per-label file and echoes it.
run_bench() {
    local label=$1 port=$2 identity=$3
    shift 3
    /usr/local/bin/diameterbench \
        --host 127.0.0.1 \
        --port "$port" \
        --id "$identity" \
        --realm "backend.realm" \
        --conn "$BENCH_CONNECTIONS" \
        --rate "$BENCH_RATE" \
        "$@" > "/tmp/$label.out" 2>&1
    return $?
}

# A warmup run absorbs connection setup, peer state machine startup and the
# first-touch cost of every lazily built index, none of which the steady-state
# figures should carry.
echo ""
echo -e "${BLUE}[2/3] Warming up${NC}"
run_bench warmup-baseline 3869 "baseline.test.realm" --duration "$BENCH_WARMUP"
run_bench warmup-dea 3870 "dea.test.realm" --duration "$BENCH_WARMUP" --origin-realm "$PARTNER_REALM"
echo -e "${GREEN}  Warmup complete${NC}"

# backend_rx <peer identity> -- messages the backend received from that agent.
# Labelled per peer, so the two arms can be told apart at the backend rather
# than inferred from a single shared total. The label match is anchored on a
# delimiter rather than on position, because Prometheus emits labels in
# alphabetical order and "partner" sorts ahead of "peer".
backend_rx() {
    curl -s --max-time 2 http://127.0.0.1:9091/metrics 2>/dev/null \
        | grep '^diameter_peer_messages_received_total{' \
        | grep "[{,]peer=\"$1\"" \
        | awk '{s += $NF} END {printf "%d", s+0}'
}

echo ""
echo -e "${BLUE}[3/3] Measuring${NC}"
echo -e "  ${YELLOW}baseline${NC}: plain relay, no edge model"
B_BACKEND_BEFORE=$(backend_rx "baseline.test.realm")
run_bench baseline 3869 "baseline.test.realm" --duration "$BENCH_DURATION"
BASELINE_RC=$?
B_BACKEND_AFTER=$(backend_rx "baseline.test.realm")
echo -e "  ${YELLOW}dea${NC}: screening + rate limiting + topology hiding + edge routing"

# Peer gauges only mean anything while the traffic is flowing; sampling after
# the benchmark exits would report every partner peer as down.
(
    peak=0
    while true; do
        n=$(curl -s --max-time 1 http://127.0.0.1:9092/metrics 2>/dev/null \
            | grep '^diameter_edge_partner_peers_up' | awk '{s += $NF} END {printf "%d", s+0}')
        [ "${n:-0}" -gt "$peak" ] && peak=$n && echo "$peak" > /tmp/peers_peak
        sleep 0.5
    done
) &
SAMPLER=$!
echo 0 > /tmp/peers_peak

D_BACKEND_BEFORE=$(backend_rx "dea.test.realm")
run_bench dea 3870 "dea.test.realm" --duration "$BENCH_DURATION" --origin-realm "$PARTNER_REALM"
DEA_RC=$?
D_BACKEND_AFTER=$(backend_rx "dea.test.realm")
kill $SAMPLER 2>/dev/null || true

# ---------------------------------------------------------------------------
# Results
# ---------------------------------------------------------------------------

field() { grep "^$2" "/tmp/$1.out" | head -1 | sed "s/^$2[[:space:]]*//"; }

# to_ms normalises a Go duration ("1.2ms", "873µs", "1.5s") to milliseconds so
# the two runs can be compared numerically.
to_ms() {
    local v=$1
    python3 - "$v" <<'PY'
import re, sys
v = sys.argv[1].strip()
m = re.match(r'^([0-9.]+)(ns|us|\u00b5s|ms|s|m)$', v)
if not m:
    print("")
    sys.exit()
n, u = float(m.group(1)), m.group(2)
print("%.3f" % (n * {"ns":1e-6, "us":1e-3, "\u00b5s":1e-3, "ms":1.0, "s":1e3, "m":6e4}[u]))
PY
}

pct() {
    python3 -c "
import sys
b, d = sys.argv[1], sys.argv[2]
if not b or not d or float(b) == 0:
    print('n/a')
else:
    print('%+.1f%%' % ((float(d) - float(b)) / float(b) * 100))
" "$1" "$2"
}

row() {
    local name=$1 base=$2 dea=$3 delta
    delta=$(pct "$base" "$dea")
    printf "  %-22s %14s %14s %12s\n" "$name" "$base" "$dea" "$delta"
}

echo ""
echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  Results${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""
printf "  %-22s %14s %14s %12s\n" "" "baseline" "DEA" "delta"
printf "  %-22s %14s %14s %12s\n" "----------------------" "--------------" "--------------" "------------"

B_TP=$(field baseline "Throughput:" | sed 's/ req\/sec//')
D_TP=$(field dea "Throughput:" | sed 's/ req\/sec//')
row "throughput (req/s)" "$B_TP" "$D_TP"

for stat in Avg P50 P95 P99 Max; do
    b=$(to_ms "$(field baseline "$stat Latency:")")
    d=$(to_ms "$(field dea "$stat Latency:")")
    row "$(echo "$stat" | tr 'A-Z' 'a-z') latency (ms)" "$b" "$d"
done

B_SENT=$(field baseline "Total Requests Sent:")
D_SENT=$(field dea "Total Requests Sent:")
B_RECV=$(field baseline "Total Answers Recv:")
D_RECV=$(field dea "Total Answers Recv:")
echo ""
printf "  %-22s %14s %14s\n" "requests sent" "$B_SENT" "$D_SENT"
printf "  %-22s %14s %14s\n" "answers received" "$B_RECV" "$D_RECV"

# ---------------------------------------------------------------------------
# Validity checks
#
# A throughput number is only meaningful if the edge agent actually did the
# work. An agent that rejects every request, or that silently stops hiding
# topology, would post excellent figures for the wrong reason.
# ---------------------------------------------------------------------------

echo ""
echo -e "${BLUE}--- Validity ---${NC}"

check() {
    if [ "$2" = "pass" ]; then
        echo -e "  ${GREEN}✓${NC} $1"
    else
        echo -e "  ${RED}✗${NC} $1"
        FAILURES=$((FAILURES + 1))
    fi
}

DEA_METRICS=$(curl -s http://127.0.0.1:9092/metrics 2>/dev/null)
metric() {
    echo "$DEA_METRICS" | grep "^$1" | awk '{s += $NF} END {printf "%d", s+0}'
}

[ "${D_RECV:-0}" -gt 0 ] 2>/dev/null && check "the edge agent answered requests" pass \
    || check "the edge agent answered requests (got ${D_RECV:-0})" fail

# Answered but not screened out: a rejection is generated locally and would
# still be counted as an answer by the benchmark.
REJECTED=$(metric "diameter_ext_dea_screening_rejected_total")
if [ "${REJECTED:-0}" -eq 0 ]; then
    check "no request was rejected by screening" pass
else
    check "no request was rejected by screening (got $REJECTED; the partner model is wrong)" fail
fi

LIMITED=$(metric "diameter_ext_dea_ratelimit_rejected_total")
if [ "${LIMITED:-0}" -eq 0 ]; then
    check "no request was throttled" pass
else
    check "no request was throttled (got $LIMITED; raise the partner ceiling)" fail
fi

PEERS_PEAK=$(cat /tmp/peers_peak 2>/dev/null || echo 0)
if [ "${PEERS_PEAK:-0}" -ge "$BENCH_CONNECTIONS" ]; then
    check "all $BENCH_CONNECTIONS partner peers were bound to their partner under load" pass
else
    check "all $BENCH_CONNECTIONS partner peers were bound to their partner under load (peaked at $PEERS_PEAK)" fail
fi

HIDDEN=$(metric "diameter_ext_dea_topohide_hidden_answers_total")
if [ "${HIDDEN:-0}" -gt 0 ]; then
    check "topology hiding ran on the measured traffic ($HIDDEN messages)" pass
else
    check "topology hiding ran on the measured traffic (got 0)" fail
fi

# The point of the comparison is that both arms do the same work end to end.
# An edge agent that answered locally -- because a control short-circuited, or
# because the internal peer was never reachable -- would post excellent figures
# for traffic that never left the edge. Summing a shared backend total cannot
# detect that: the baseline arm alone keeps it above zero. Each arm is
# therefore measured separately, at the backend, over its own run.
D_BACKEND_DELTA=$((${D_BACKEND_AFTER:-0} - ${D_BACKEND_BEFORE:-0}))
B_BACKEND_DELTA=$((${B_BACKEND_AFTER:-0} - ${B_BACKEND_BEFORE:-0}))

if [ "${D_SENT:-0}" -gt 0 ] && [ "$D_BACKEND_DELTA" -ge "${D_SENT:-0}" ]; then
    check "every measured DEA request was relayed to the internal peer ($D_BACKEND_DELTA >= $D_SENT)" pass
else
    check "every measured DEA request was relayed to the internal peer (backend saw $D_BACKEND_DELTA of $D_SENT; the edge answered locally)" fail
fi

if [ "${B_SENT:-0}" -gt 0 ] && [ "$B_BACKEND_DELTA" -ge "${B_SENT:-0}" ]; then
    check "every measured baseline request was relayed to the internal peer ($B_BACKEND_DELTA >= $B_SENT)" pass
else
    check "every measured baseline request was relayed to the internal peer (backend saw $B_BACKEND_DELTA of $B_SENT)" fail
fi

# Edge routing evaluates direction on every outgoing message, but in this
# external-to-internal topology its outcome counters legitimately stay at zero,
# so only the evaluation counter shows it ran.
ROUTE_EVAL=$(metric "diameter_ext_dea_route_evaluated_total")
if [ "${ROUTE_EVAL:-0}" -gt 0 ]; then
    check "edge routing evaluated the measured traffic ($ROUTE_EVAL messages)" pass
else
    check "edge routing evaluated the measured traffic (got 0)" fail
fi

# The internal peer must be classified as internal, not merely reachable: the
# external-to-internal direction rule and topology hiding both key on the zone.
INTERNAL_UP=$(echo "$DEA_METRICS" \
    | grep '^diameter_peer_up{' \
    | grep '[{,]peer="server.backend.realm"' | grep '[{,]zone="core"' \
    | awk '{s += $NF} END {printf "%d", s+0}')
if [ "${INTERNAL_UP:-0}" -ge 1 ]; then
    check "the internal peer was up in the internal zone" pass
else
    check "the internal peer was up in the internal zone (not up, or not tagged zone=core)" fail
fi

# Stateful hiding allocates per session, so its store grows with offered load
# rather than with topology size. Reporting the ratio makes the capacity
# question visible instead of leaving it to be discovered in production.
ALLOC=$(metric "diameter_ext_dea_topohide_pseudonyms_allocated_total")
HELD=$(metric "diameter_ext_dea_topohide_store_pseudonyms")
EVICTED=$(metric "diameter_ext_dea_topohide_store_evictions_total")
SCREENED=$(metric "diameter_ext_dea_screening_screened_total")
echo ""
echo -e "${BLUE}--- Edge state growth ---${NC}"
printf "  %-34s %12s\n" "requests screened" "${SCREENED:-0}"
printf "  %-34s %12s\n" "pseudonyms allocated" "${ALLOC:-0}"
printf "  %-34s %12s\n" "pseudonyms held" "${HELD:-0}"
printf "  %-34s %12s\n" "store evictions" "${EVICTED:-0}"

echo ""
echo -e "${BLUE}--- Edge metrics ---${NC}"
echo "$DEA_METRICS" | grep -E '^diameter_(edge|ext_dea)_' | head -40 || echo "  (unavailable)"

echo ""
echo -e "${BLUE}==============================================${NC}"
if [ "$FAILURES" -eq 0 ] && [ "$BASELINE_RC" -eq 0 ] && [ "$DEA_RC" -eq 0 ]; then
    echo -e "${GREEN}  Performance test completed successfully${NC}"
    echo -e "${BLUE}==============================================${NC}"
    exit 0
fi
echo -e "${RED}  Performance test failed ($FAILURES invalid result(s))${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""
echo -e "${YELLOW}--- edge agent log (tail) ---${NC}"
tail -30 /tmp/dea.log
exit 1
