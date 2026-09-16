#!/bin/bash
# Entrypoint for the godiam DEA soak test.
#
# Drives a Diameter Edge Agent through a long run with every dea_* control
# enabled, sampling CPU, RSS, heap and goroutines throughout, and judges the
# result on resource behaviour rather than on throughput.
#
# The phases exist to separate classes of resource bug that steady load alone
# will not distinguish:
#
#   baseline  idle after startup, to know what "empty" costs
#   steady    sustained load, where unbounded growth shows as a slope
#   churn     repeated connect/disconnect, where per-peer leaks accumulate
#   burst     a rate spike, to check the agent sheds load without retaining it
#   drain     idle for longer than the pseudonym TTL, where anything that was
#             merely in use is released and anything leaked is not

set -u

GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

PHASE_DURATION=${PHASE_DURATION:-120}
BENCH_RATE=${BENCH_RATE:-1000}
BENCH_CONN=${BENCH_CONN:-10}
BURST_RATE=${BURST_RATE:-5000}
CHURN_CYCLES=${CHURN_CYCLES:-8}
SAMPLE_INTERVAL=${SAMPLE_INTERVAL:-2}

# Must match topology_hiding.ttl and max_entries in conf/dea.yaml: the drain
# phase has to outlast the TTL for the sweeper to be observable.
TTL_SECONDS=30
MAX_ENTRIES=20000

PARTNER_REALM="soak.partner.net"
PHASES="baseline,steady,churn,burst,drain"
FAILURES=0

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam DEA Soak Test${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""
echo "  Phase duration: ${PHASE_DURATION}s"
echo "  Rate:           $BENCH_RATE req/s over $BENCH_CONN connections"
echo "  Burst:          $BURST_RATE req/s"
echo "  Churn cycles:   $CHURN_CYCLES"
echo "  Sampling:       every ${SAMPLE_INTERVAL}s"
echo ""

: > /tmp/pids
cleanup() {
    echo done > /tmp/phase 2>/dev/null || true
    while read -r pid; do kill "$pid" 2>/dev/null || true; done < /tmp/pids
}
trap cleanup EXIT

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

echo -e "${BLUE}[1/3] Starting nodes${NC}"
start_node "backend server" /conf/server.yaml /tmp/server.log || exit 1
SERVER_PID=$(tail -1 /tmp/pids)
start_node "edge agent" /conf/dea.yaml /tmp/dea.log || exit 1
DEA_PID=$(tail -1 /tmp/pids)
sleep 3

phase() {
    echo "$1" > /tmp/phase
    echo -e "  ${YELLOW}$1${NC}: $2"
}

echo ""
echo -e "${BLUE}[2/3] Running phases${NC}"

echo baseline > /tmp/phase
python3 /monitor.py http://127.0.0.1:9092/metrics /tmp/samples.csv /tmp/phase "$SAMPLE_INTERVAL" &
echo $! >> /tmp/pids

load() {
    /usr/local/bin/diameterbench \
        --host 127.0.0.1 --port 3870 --id "dea.test.realm" \
        --realm "backend.realm" --origin-realm "$PARTNER_REALM" \
        --conn "$2" --rate "$3" --duration "$1" > "/tmp/$4.out" 2>&1
}

# baseline: idle, so the sampler records what the process costs before work.
phase baseline "idle, establishing the empty-process cost"
sleep 30

phase steady "sustained ${BENCH_RATE} req/s for ${PHASE_DURATION}s"
load "${PHASE_DURATION}s" "$BENCH_CONN" "$BENCH_RATE" steady
STEADY_RC=$?

# churn: each cycle is a full connect, traffic, disconnect. Peer goroutines,
# registry bindings and limiter buckets are all created per connection, so
# anything not released accumulates across cycles.
phase churn "$CHURN_CYCLES connect/disconnect cycles"
for i in $(seq 1 "$CHURN_CYCLES"); do
    load "5s" "$BENCH_CONN" "$BENCH_RATE" "churn-$i"
    sleep 1
done

# Delivery is asserted separately for the phases that stay within capacity and
# for the burst, which deliberately does not. Reading the backend counter here
# splits the two.
backend_rx() {
    curl -s --max-time 3 http://127.0.0.1:9091/metrics 2>/dev/null \
        | grep '^diameter_peer_messages_received_total{' | grep '[{,]peer="dea.test.realm"' \
        | awk '{s += $NF} END {printf "%d", s+0}'
}
BACKEND_RX_PREBURST=$(backend_rx)

phase burst "spike to ${BURST_RATE} req/s"
load "$((PHASE_DURATION / 3))s" "$((BENCH_CONN * 3))" "$BURST_RATE" burst

# Delivery is measured here rather than at the end of the run: it is a property
# of the traffic phases, and reading it after a long idle drain would confuse a
# backend that went away later with one that was never reached.
BACKEND_RX=$(backend_rx)

# drain: idle for longer than both the pseudonym TTL and Go's 2-minute forced
# GC period. The second bound matters as much as the first: an idle Go process
# allocates nothing, so nothing triggers a GC cycle, and heap_inuse would sit at
# its peak for reasons that have nothing to do with a leak.
DRAIN_SECONDS=${DRAIN_SECONDS:-150}
if [ "$DRAIN_SECONDS" -lt $((TTL_SECONDS * 3)) ]; then
    DRAIN_SECONDS=$((TTL_SECONDS * 3))
fi
phase drain "idle for ${DRAIN_SECONDS}s, past the ${TTL_SECONDS}s TTL and the forced GC"
sleep "$DRAIN_SECONDS"

echo done > /tmp/phase
sleep 1

echo ""
echo -e "${BLUE}[3/3] Results${NC}"

if kill -0 "$DEA_PID" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} the edge agent survived the run"
else
    echo -e "  ${RED}✗${NC} the edge agent died during the run"
    FAILURES=$((FAILURES + 1))
fi

if kill -0 "$SERVER_PID" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} the backend survived the run"
else
    echo -e "  ${RED}✗${NC} the backend died during the run"
    FAILURES=$((FAILURES + 1))
fi

METRICS=$(curl -s --max-time 3 http://127.0.0.1:9092/metrics 2>/dev/null)
metric() { echo "$METRICS" | grep "^$1" | awk '{s += $NF} END {printf "%d", s+0}'; }

REJECTED=$(metric "diameter_ext_dea_screening_rejected_total")
LIMITED=$(metric "diameter_ext_dea_ratelimit_rejected_total")
SCREENED=$(metric "diameter_ext_dea_screening_screened_total")
ALLOC=$(metric "diameter_ext_dea_topohide_pseudonyms_allocated_total")
HIDDEN=$(metric "diameter_ext_dea_topohide_hidden_answers_total")
SENT_STEADY=0
for f in /tmp/steady.out /tmp/churn-*.out; do
    [ -f "$f" ] || continue
    n=$(grep "^Total Requests Sent:" "$f" | awk '{print $NF}')
    SENT_STEADY=$((SENT_STEADY + ${n:-0}))
done
SENT_BURST=0
if [ -f /tmp/burst.out ]; then
    SENT_BURST=$(grep "^Total Requests Sent:" /tmp/burst.out | awk '{print $NF}')
    SENT_BURST=${SENT_BURST:-0}
fi
SENT=$((SENT_STEADY + SENT_BURST))
BURST_RX=$((BACKEND_RX - BACKEND_RX_PREBURST))
ROUTING_ERRORS=$(grep -c "Routing error" /tmp/dea.log 2>/dev/null) || ROUTING_ERRORS=0

echo ""
echo -e "${BLUE}--- Work done ---${NC}"
printf "  %-34s %12s\n" "requests sent by the clients" "${SENT:-0}"
printf "  %-34s %12s\n" "requests screened" "${SCREENED:-0}"
printf "  %-34s %12s\n" "relayed to the internal peer" "${BACKEND_RX:-0}"
printf "  %-34s %12s\n" "answers topology-hidden" "${HIDDEN:-0}"
printf "  %-34s %12s\n" "pseudonyms allocated" "${ALLOC:-0}"
printf "  %-34s %12s\n" "rejected by screening" "${REJECTED:-0}"
printf "  %-34s %12s\n" "throttled" "${LIMITED:-0}"

# A soak that quietly stopped doing the work would look excellent in every
# resource graph, so the same guard the perf test uses applies here.
if [ "${SCREENED:-0}" -gt 0 ] && [ "${REJECTED:-0}" -eq 0 ] && [ "${LIMITED:-0}" -eq 0 ]; then
    echo -e "  ${GREEN}✓${NC} the controls ran on the traffic and rejected none of it"
else
    echo -e "  ${RED}✗${NC} the controls did not run cleanly (screened ${SCREENED:-0}, rejected ${REJECTED:-0}, throttled ${LIMITED:-0})"
    FAILURES=$((FAILURES + 1))
fi

# Counting answers is not enough: an agent that cannot reach its backend
# answers UNABLE_TO_DELIVER itself, and the client counts those as answers.
# Delivery has to be confirmed at the backend and at the hiding counters,
# which only move for answers that actually came back from an internal host.
# Within capacity, delivery is absolute: an agent that cannot deliver answers
# UNABLE_TO_DELIVER itself, and the client counts that as an answer.
if [ "${SENT_STEADY:-0}" -gt 0 ] && [ "${BACKEND_RX_PREBURST:-0}" -ge "${SENT_STEADY:-0}" ]; then
    echo -e "  ${GREEN}✓${NC} every steady and churn request was relayed to the internal peer (${BACKEND_RX_PREBURST} >= ${SENT_STEADY})"
else
    echo -e "  ${RED}✗${NC} requests were not relayed to the internal peer (backend saw ${BACKEND_RX_PREBURST:-0} of ${SENT_STEADY:-0}; the edge answered locally)"
    FAILURES=$((FAILURES + 1))
fi

# The burst deliberately offers several times the steady rate against a
# pseudonym store held at max_entries, so every request allocates and evicts.
# Demanding zero loss there would assert capacity the run is designed to exceed;
# what must hold is that the agent sheds gracefully rather than collapsing.
BURST_FLOOR=$((SENT_BURST * 60 / 100))
if [ "${SENT_BURST:-0}" -eq 0 ] || [ "${BURST_RX:-0}" -ge "$BURST_FLOOR" ]; then
    echo -e "  ${GREEN}✓${NC} the burst was absorbed rather than collapsing (${BURST_RX} of ${SENT_BURST} delivered)"
else
    echo -e "  ${RED}✗${NC} the burst collapsed (backend saw only ${BURST_RX} of ${SENT_BURST})"
    FAILURES=$((FAILURES + 1))
fi

# Two failures are expected from what the phases deliberately do: a next hop
# refusing a message under the burst, and an answer arriving for a client the
# churn phase has just disconnected. Anything else means a route or a peer was
# lost for a reason the run did not cause, which is what this check is for.
OTHER_ERRORS=$(grep "Routing error" /tmp/dea.log 2>/dev/null \
    | grep -v "refused the message" \
    | grep -vc "peer not in open state") || OTHER_ERRORS=0
if [ "${OTHER_ERRORS:-0}" -eq 0 ]; then
    if [ "${ROUTING_ERRORS:-0}" -eq 0 ]; then
        echo -e "  ${GREEN}✓${NC} no routing errors were logged"
    else
        echo -e "  ${GREEN}✓${NC} routing errors were all next-hop backpressure under the burst ($ROUTING_ERRORS occurrences)"
    fi
else
    echo -e "  ${RED}✗${NC} the agent lost a route or a peer ($OTHER_ERRORS of $ROUTING_ERRORS errors were not backpressure)"
    FAILURES=$((FAILURES + 1))
fi

python3 /analyze.py /tmp/samples.csv "$MAX_ENTRIES" "$PHASES"
ANALYSIS_RC=$?

echo ""
echo -e "${BLUE}==============================================${NC}"
if [ "$FAILURES" -eq 0 ] && [ "$ANALYSIS_RC" -eq 0 ] && [ "${STEADY_RC:-0}" -eq 0 ]; then
    echo -e "${GREEN}  Soak test completed successfully${NC}"
    echo -e "${BLUE}==============================================${NC}"
    exit 0
fi
echo -e "${RED}  Soak test failed${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""
echo -e "${YELLOW}--- edge agent log (tail) ---${NC}"
tail -30 /tmp/dea.log
exit 1
