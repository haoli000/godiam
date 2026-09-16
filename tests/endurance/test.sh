#!/bin/bash
# Endurance test validation script
# Runs 7 phases simulating production scenarios

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

CHECKS_PASSED=0
CHECKS_TOTAL=0
PHASE_NUM=0

check() {
    CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
    local desc="$1"
    local result="$2"
    if [ "$result" = "true" ]; then
        CHECKS_PASSED=$((CHECKS_PASSED + 1))
        echo -e "  ${GREEN}✓${NC} $desc"
    else
        echo -e "  ${RED}✗${NC} $desc"
    fi
}

phase_header() {
    PHASE_NUM=$((PHASE_NUM + 1))
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  Phase $PHASE_NUM: $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

run_bench() {
    local label="$1"
    local dur="$2"
    local rate="$3"
    local conn="${4:-$BENCH_CONN}"

    echo -e "${CYAN}  Running diameterbench: ${dur}s @ ${rate} req/s (${conn} conns)${NC}"
    /usr/local/bin/diameterbench \
        --host 127.0.0.1 \
        --port 3869 \
        --id "dra.test.realm" \
        --realm "backend.realm" \
        --conn "$conn" \
        --duration "${dur}s" \
        --rate "$rate" \
        --connect-retries 5 \
        --connect-timeout 5s \
        > "/tmp/bench_${label}.log" 2>&1 || true

    # Parse results
    local sent=$(grep "Total Requests Sent:" "/tmp/bench_${label}.log" | awk '{print $NF}')
    local recv=$(grep "Total Answers Recv:" "/tmp/bench_${label}.log" | awk '{print $NF}')
    local throughput=$(grep "Throughput:" "/tmp/bench_${label}.log" | awk '{print $2}')
    local p99=$(grep "P99 Latency:" "/tmp/bench_${label}.log" | awk '{print $NF}')

    echo -e "  ${CYAN}Results: sent=$sent recv=$recv throughput=$throughput p99=$p99${NC}"

    # Export for validation
    export BENCH_SENT="$sent"
    export BENCH_RECV="$recv"
    export BENCH_THROUGHPUT="$throughput"
    export BENCH_P99="$p99"
}

scrape_metrics() {
    local label="$1"
    curl -s http://127.0.0.1:9090/metrics 2>/dev/null > "/tmp/metrics_dra_${label}_full.txt" || true
    curl -s http://127.0.0.1:9091/metrics 2>/dev/null > "/tmp/metrics_server1_${label}_full.txt" || true
    curl -s http://127.0.0.1:9092/metrics 2>/dev/null > "/tmp/metrics_server2_${label}_full.txt" || true
    grep '^diameter_' "/tmp/metrics_dra_${label}_full.txt" > "/tmp/metrics_dra_${label}.txt" 2>/dev/null || true
    grep '^diameter_' "/tmp/metrics_server1_${label}_full.txt" > "/tmp/metrics_server1_${label}.txt" 2>/dev/null || true
    grep '^diameter_' "/tmp/metrics_server2_${label}_full.txt" > "/tmp/metrics_server2_${label}.txt" 2>/dev/null || true
}

get_metric() {
    local file="$1"
    local metric="$2"
    grep "^${metric}" "$file" 2>/dev/null | head -1 | awk '{print $NF}' || echo "0"
}

get_runtime_metric() {
    local port="$1"
    local metric="$2"
    curl -s "http://127.0.0.1:${port}/metrics" 2>/dev/null | grep "^${metric} " | awk '{print $NF}' || echo "0"
}

# Memory and goroutine snapshot for a service
snapshot_runtime() {
    local label="$1"
    local service="$2"
    local port="$3"

    local heap=$(get_runtime_metric "$port" "go_memstats_heap_alloc_bytes")
    local sys=$(get_runtime_metric "$port" "go_memstats_sys_bytes")
    local goroutines=$(get_runtime_metric "$port" "go_goroutines")
    local gc_count=$(get_runtime_metric "$port" "go_gc_duration_seconds_count")
    local heap_objects=$(get_runtime_metric "$port" "go_memstats_heap_objects")

    local heap_mb=$(echo "$heap" | awk '{printf "%.2f", $1/1048576}')
    local sys_mb=$(echo "$sys" | awk '{printf "%.2f", $1/1048576}')

    echo -e "  ${CYAN}${service}: heap=${heap_mb}MB sys=${sys_mb}MB goroutines=${goroutines} gc=${gc_count} objects=${heap_objects}${NC}"

    # Save for later comparison
    echo "${heap}" > "/tmp/rt_${service}_${label}_heap.txt"
    echo "${goroutines}" > "/tmp/rt_${service}_${label}_goroutines.txt"
    echo "${heap_objects}" > "/tmp/rt_${service}_${label}_objects.txt"
    echo "${sys}" > "/tmp/rt_${service}_${label}_sys.txt"
}

snapshot_all() {
    local label="$1"
    echo -e "  ${CYAN}--- Runtime snapshot: ${label} ---${NC}"
    snapshot_runtime "$label" "dra" 9090
    snapshot_runtime "$label" "server1" 9091
    snapshot_runtime "$label" "server2" 9092
}

get_saved() {
    cat "/tmp/rt_${1}_${2}_${3}.txt" 2>/dev/null | awk '{printf "%d", $1+0}' || echo "0"
}

processes_alive() {
    local all_alive="true"
    kill -0 $PID_DRA 2>/dev/null || all_alive="false"
    echo "$all_alive"
}

# ============================================================
# PHASE 1: Baseline - Steady State
# ============================================================
phase_header "Baseline — Steady State"
echo -e "  Sustained traffic at ${BENCH_RATE} req/s for ${PHASE_DURATION}s"

snapshot_all "pre"
scrape_metrics "pre"
run_bench "baseline" "$PHASE_DURATION" "$BENCH_RATE"
scrape_metrics "baseline"
snapshot_all "baseline"

# Validate baseline
check "DRA process alive" "$(processes_alive)"

# Check both servers received traffic
S1_DISPATCHED=$(get_metric "/tmp/metrics_server1_baseline.txt" "diameter_routing_dispatched_total")
S2_DISPATCHED=$(get_metric "/tmp/metrics_server2_baseline.txt" "diameter_routing_dispatched_total")
echo -e "  ${CYAN}server1 dispatched: $S1_DISPATCHED, server2 dispatched: $S2_DISPATCHED${NC}"

S1_NUM=$(echo "$S1_DISPATCHED" | awk '{printf "%d", $1+0}')
S2_NUM=$(echo "$S2_DISPATCHED" | awk '{printf "%d", $1+0}')
check "server1 received traffic ($S1_NUM msgs)" "$([ "$S1_NUM" -gt 0 ] && echo true || echo false)"
check "server2 received traffic ($S2_NUM msgs)" "$([ "$S2_NUM" -gt 0 ] && echo true || echo false)"

# Check throughput is reasonable (>= 50% of target)
if [ -n "$BENCH_RECV" ] && [ "$BENCH_RECV" -gt 0 ] 2>/dev/null; then
    MIN_EXPECTED=$(( BENCH_RATE * PHASE_DURATION / 2 ))
    check "Throughput acceptable (recv=$BENCH_RECV >= $MIN_EXPECTED)" \
        "$([ "$BENCH_RECV" -ge "$MIN_EXPECTED" ] && echo true || echo false)"
else
    check "Throughput acceptable (recv=$BENCH_RECV)" "false"
fi

# Check no routing errors
DRA_ERRORS=$(get_metric "/tmp/metrics_dra_baseline.txt" "diameter_routing_errors_total")
DRA_ERR_NUM=$(echo "$DRA_ERRORS" | awk '{printf "%d", $1+0}')
check "No routing errors (errors=$DRA_ERR_NUM)" "$([ "$DRA_ERR_NUM" -eq 0 ] && echo true || echo false)"

BASELINE_S1=$S1_NUM
BASELINE_S2=$S2_NUM

# ============================================================
# PHASE 2: Server Failure
# ============================================================
phase_header "Server Failure — Kill server1"
echo -e "  Killing server1, traffic should failover to server2"

kill $PID_SERVER1 2>/dev/null || true
wait $PID_SERVER1 2>/dev/null || true
sleep 2

run_bench "failure" "$PHASE_DURATION" "$BENCH_RATE"
scrape_metrics "failure"
snapshot_all "failure"

check "DRA process survived server1 crash" "$(processes_alive)"

# server2 should have received all new traffic
S2_AFTER=$(get_metric "/tmp/metrics_server2_failure.txt" "diameter_routing_dispatched_total")
S2_AFTER_NUM=$(echo "$S2_AFTER" | awk '{printf "%d", $1+0}')
S2_NEW=$((S2_AFTER_NUM - BASELINE_S2))
check "server2 handled failover traffic ($S2_NEW new msgs)" \
    "$([ "$S2_NEW" -gt 0 ] && echo true || echo false)"

check "Bench received answers during failover (recv=$BENCH_RECV)" \
    "$([ -n "$BENCH_RECV" ] && [ "$BENCH_RECV" -gt 0 ] 2>/dev/null && echo true || echo false)"

FAILURE_S2=$S2_AFTER_NUM

# ============================================================
# PHASE 3: Server Recovery
# ============================================================
phase_header "Server Recovery — Restart server1"
echo -e "  Restarting server1, traffic should rebalance"

/usr/local/bin/diameterd --config /conf/server1.yaml > /tmp/server1.log 2>&1 &
PID_SERVER1=$!
sleep 3

if ! kill -0 $PID_SERVER1 2>/dev/null; then
    echo -e "${YELLOW}  Warning: server1 failed to restart${NC}"
fi

run_bench "recovery" "$PHASE_DURATION" "$BENCH_RATE"
scrape_metrics "recovery"
snapshot_all "recovery"

check "DRA process alive after recovery" "$(processes_alive)"

# Check server1 received some traffic (reconnected)
S1_RECOVERED=$(get_metric "/tmp/metrics_server1_recovery.txt" "diameter_routing_dispatched_total")
S1_REC_NUM=$(echo "$S1_RECOVERED" | awk '{printf "%d", $1+0}')
check "server1 received traffic after restart ($S1_REC_NUM msgs)" \
    "$([ "$S1_REC_NUM" -gt 0 ] && echo true || echo false)"

S2_RECOVERY=$(get_metric "/tmp/metrics_server2_recovery.txt" "diameter_routing_dispatched_total")
S2_REC_NUM=$(echo "$S2_RECOVERY" | awk '{printf "%d", $1+0}')
S2_REC_NEW=$((S2_REC_NUM - FAILURE_S2))
check "server2 still receiving traffic ($S2_REC_NEW new msgs)" \
    "$([ "$S2_REC_NEW" -gt 0 ] && echo true || echo false)"

RECOVERY_S1=$S1_REC_NUM
RECOVERY_S2=$S2_REC_NUM

# ============================================================
# PHASE 4: Network Partition — Drop packets to server2
# ============================================================
phase_header "Network Partition — Drop packets to server2"
echo -e "  Using iptables to drop packets to port 3871"

HAS_IPTABLES="true"
iptables -A OUTPUT -p tcp --dport 3871 -j DROP 2>/dev/null || HAS_IPTABLES="false"

if [ "$HAS_IPTABLES" = "true" ]; then
    sleep 2
    run_bench "partition" "$PHASE_DURATION" "$BENCH_RATE"
    scrape_metrics "partition"

    check "DRA survived network partition" "$(processes_alive)"

    # server1 should get all new traffic
    S1_PART=$(get_metric "/tmp/metrics_server1_partition.txt" "diameter_routing_dispatched_total")
    S1_PART_NUM=$(echo "$S1_PART" | awk '{printf "%d", $1+0}')
    S1_PART_NEW=$((S1_PART_NUM - RECOVERY_S1))
    check "server1 handled rerouted traffic ($S1_PART_NEW new msgs)" \
        "$([ "$S1_PART_NEW" -gt 0 ] && echo true || echo false)"

    check "Bench received answers during partition (recv=$BENCH_RECV)" \
        "$([ -n "$BENCH_RECV" ] && [ "$BENCH_RECV" -gt 0 ] 2>/dev/null && echo true || echo false)"

    PARTITION_S1=$S1_PART_NUM
    PARTITION_S2=$RECOVERY_S2
else
    echo -e "${YELLOW}  Skipping: iptables not available (no NET_ADMIN capability)${NC}"
    echo -e "${YELLOW}  Run with: docker run --cap-add NET_ADMIN ...${NC}"
    PARTITION_S1=$RECOVERY_S1
    PARTITION_S2=$RECOVERY_S2
fi

# ============================================================
# PHASE 5: Network Recovery
# ============================================================
phase_header "Network Recovery — Restore server2 connectivity"

if [ "$HAS_IPTABLES" = "true" ]; then
    echo -e "  Removing iptables DROP rule"
    iptables -D OUTPUT -p tcp --dport 3871 -j DROP 2>/dev/null || true
    sleep 3

    run_bench "net_recovery" "$PHASE_DURATION" "$BENCH_RATE"
    scrape_metrics "net_recovery"

    check "DRA alive after network recovery" "$(processes_alive)"

    # Both servers should receive traffic again
    S2_NETREC=$(get_metric "/tmp/metrics_server2_net_recovery.txt" "diameter_routing_dispatched_total")
    S2_NETREC_NUM=$(echo "$S2_NETREC" | awk '{printf "%d", $1+0}')
    S2_NETREC_NEW=$((S2_NETREC_NUM - PARTITION_S2))
    check "server2 resumed receiving traffic ($S2_NETREC_NEW new msgs)" \
        "$([ "$S2_NETREC_NEW" -gt 0 ] && echo true || echo false)"

    NETREC_S1=$(get_metric "/tmp/metrics_server1_net_recovery.txt" "diameter_routing_dispatched_total")
    NETREC_S1_NUM=$(echo "$NETREC_S1" | awk '{printf "%d", $1+0}')
else
    echo -e "${YELLOW}  Skipping: iptables was not available${NC}"
fi

# ============================================================
# PHASE 6: Overload Burst
# ============================================================
phase_header "Overload Burst — Spike to ${BURST_RATE} req/s"
echo -e "  Testing system resilience under sudden load spike"

BURST_DURATION=$(( PHASE_DURATION * 3 / 4 ))
[ "$BURST_DURATION" -lt 5 ] && BURST_DURATION=5

run_bench "burst" "$BURST_DURATION" "$BURST_RATE" "$BENCH_CONN"
scrape_metrics "burst"
snapshot_all "burst"

check "DRA survived overload burst" "$(processes_alive)"
check "server1 still alive after burst" \
    "$(kill -0 $PID_SERVER1 2>/dev/null && echo true || echo false)"
check "server2 still alive after burst" \
    "$(kill -0 $PID_SERVER2 2>/dev/null && echo true || echo false)"

check "Bench received answers during burst (recv=$BENCH_RECV)" \
    "$([ -n "$BENCH_RECV" ] && [ "$BENCH_RECV" -gt 0 ] 2>/dev/null && echo true || echo false)"

# ============================================================
# PHASE 7: Final Metrics Validation
# ============================================================
phase_header "Final Metrics Validation"
echo -e "  Scraping all Prometheus endpoints for consistency"

scrape_metrics "final"

# DRA metrics
DRA_REQ=$(get_metric "/tmp/metrics_dra_final.txt" "diameter_routing_requests_total")
DRA_RELAYED=$(get_metric "/tmp/metrics_dra_final.txt" "diameter_routing_relayed_total")
DRA_ERRORS_FINAL=$(get_metric "/tmp/metrics_dra_final.txt" "diameter_routing_errors_total")
DRA_LOOPS=$(get_metric "/tmp/metrics_dra_final.txt" "diameter_routing_loops_detected_total")
DRA_UPTIME=$(get_metric "/tmp/metrics_dra_final.txt" "diameter_server_uptime_seconds")

echo -e "  ${CYAN}DRA total requests: $DRA_REQ${NC}"
echo -e "  ${CYAN}DRA total relayed:  $DRA_RELAYED${NC}"
echo -e "  ${CYAN}DRA total errors:   $DRA_ERRORS_FINAL${NC}"
echo -e "  ${CYAN}DRA loops detected: $DRA_LOOPS${NC}"
echo -e "  ${CYAN}DRA uptime:         ${DRA_UPTIME}s${NC}"

DRA_REQ_NUM=$(echo "$DRA_REQ" | awk '{printf "%d", $1+0}')
DRA_RELAYED_NUM=$(echo "$DRA_RELAYED" | awk '{printf "%d", $1+0}')
DRA_LOOPS_NUM=$(echo "$DRA_LOOPS" | awk '{printf "%d", $1+0}')

check "DRA processed requests (total=$DRA_REQ_NUM)" \
    "$([ "$DRA_REQ_NUM" -gt 0 ] && echo true || echo false)"
check "DRA relayed messages (total=$DRA_RELAYED_NUM)" \
    "$([ "$DRA_RELAYED_NUM" -gt 0 ] && echo true || echo false)"
check "No routing loops detected ($DRA_LOOPS_NUM)" \
    "$([ "$DRA_LOOPS_NUM" -eq 0 ] && echo true || echo false)"

# Peer connectivity
DRA_PEERS=$(grep "diameter_peers_total" "/tmp/metrics_dra_final.txt" 2>/dev/null | awk '{print $NF}')
DRA_PEERS_NUM=$(echo "$DRA_PEERS" | awk '{printf "%d", $1+0}')
check "DRA has connected peers ($DRA_PEERS_NUM)" \
    "$([ "$DRA_PEERS_NUM" -gt 0 ] && echo true || echo false)"

# Extensions loaded
DRA_EXT=$(grep 'diameter_extensions_total{state="active"}' "/tmp/metrics_dra_final.txt" 2>/dev/null | awk '{print $NF}')
DRA_EXT_NUM=$(echo "$DRA_EXT" | awk '{printf "%d", $1+0}')
check "DRA extensions active ($DRA_EXT_NUM)" \
    "$([ "$DRA_EXT_NUM" -ge 5 ] && echo true || echo false)"

# Server totals
S1_FINAL=$(get_metric "/tmp/metrics_server1_final.txt" "diameter_routing_dispatched_total")
S2_FINAL=$(get_metric "/tmp/metrics_server2_final.txt" "diameter_routing_dispatched_total")
S1_FINAL_NUM=$(echo "$S1_FINAL" | awk '{printf "%d", $1+0}')
S2_FINAL_NUM=$(echo "$S2_FINAL" | awk '{printf "%d", $1+0}')
TOTAL_DISPATCHED=$((S1_FINAL_NUM + S2_FINAL_NUM))

echo -e "  ${CYAN}server1 total dispatched: $S1_FINAL_NUM${NC}"
echo -e "  ${CYAN}server2 total dispatched: $S2_FINAL_NUM${NC}"
echo -e "  ${CYAN}Combined dispatched:      $TOTAL_DISPATCHED${NC}"

check "Servers handled significant traffic (total=$TOTAL_DISPATCHED)" \
    "$([ "$TOTAL_DISPATCHED" -gt 1000 ] && echo true || echo false)"

# ============================================================
# PHASE 8: Memory & Resource Leak Detection
# ============================================================
phase_header "Memory & Resource Leak Detection"
echo -e "  Waiting for connection cleanup cooldown..."
sleep 5
echo -e "  Comparing runtime snapshots: pre-test → post-cooldown"

snapshot_all "final"

# --- DRA Memory Analysis ---
echo ""
echo -e "  ${CYAN}--- DRA Memory Analysis ---${NC}"
DRA_HEAP_PRE=$(get_saved "dra" "pre" "heap")
DRA_HEAP_FINAL=$(get_saved "dra" "final" "heap")
DRA_GOR_PRE=$(get_saved "dra" "pre" "goroutines")
DRA_GOR_FINAL=$(get_saved "dra" "final" "goroutines")
DRA_OBJ_PRE=$(get_saved "dra" "pre" "objects")
DRA_OBJ_FINAL=$(get_saved "dra" "final" "objects")

DRA_HEAP_PRE_MB=$(echo "$DRA_HEAP_PRE" | awk '{printf "%.2f", $1/1048576}')
DRA_HEAP_FINAL_MB=$(echo "$DRA_HEAP_FINAL" | awk '{printf "%.2f", $1/1048576}')
DRA_HEAP_GROWTH=$(echo "$DRA_HEAP_PRE $DRA_HEAP_FINAL" | awk '{if ($1>0) printf "%.1f", (($2-$1)/$1)*100; else print "0"}')

echo -e "  ${CYAN}Heap:       ${DRA_HEAP_PRE_MB}MB → ${DRA_HEAP_FINAL_MB}MB (${DRA_HEAP_GROWTH}% change)${NC}"
echo -e "  ${CYAN}Goroutines: ${DRA_GOR_PRE} → ${DRA_GOR_FINAL}${NC}"
echo -e "  ${CYAN}Objects:    ${DRA_OBJ_PRE} → ${DRA_OBJ_FINAL}${NC}"

# Heap growth: allow up to 10x from baseline (Go GC is bursty, heap is not a leak indicator alone)
# But if heap is > 100MB that's suspicious for a relay
DRA_HEAP_FINAL_INT=$(echo "$DRA_HEAP_FINAL" | awk '{printf "%d", $1+0}')
check "DRA heap reasonable (<100MB, actual=${DRA_HEAP_FINAL_MB}MB)" \
    "$([ "$DRA_HEAP_FINAL_INT" -lt 104857600 ] && echo true || echo false)"

# Goroutine leak: compare final to baseline (after first bench run established connections)
# DRA handles many transient connections; use baseline as reference
DRA_GOR_BASELINE=$(get_saved "dra" "baseline" "goroutines")
DRA_GOR_LIMIT=$(( DRA_GOR_BASELINE * 3 + 20 ))
check "DRA no goroutine leak (${DRA_GOR_FINAL} <= ${DRA_GOR_LIMIT}, baseline=${DRA_GOR_BASELINE})" \
    "$([ "$DRA_GOR_FINAL" -le "$DRA_GOR_LIMIT" ] && echo true || echo false)"

# --- Server1 Memory Analysis ---
echo ""
echo -e "  ${CYAN}--- Server1 Memory Analysis ---${NC}"
S1_HEAP_FINAL=$(get_saved "server1" "final" "heap")
S1_GOR_PRE=$(get_saved "server1" "baseline" "goroutines")
S1_GOR_FINAL=$(get_saved "server1" "final" "goroutines")

# server1 was restarted in phase 3, so compare from recovery baseline
S1_HEAP_BASE=$(get_saved "server1" "recovery" "heap")
S1_HEAP_BASE_MB=$(echo "$S1_HEAP_BASE" | awk '{printf "%.2f", $1/1048576}')
S1_HEAP_FINAL_MB=$(echo "$S1_HEAP_FINAL" | awk '{printf "%.2f", $1/1048576}')

echo -e "  ${CYAN}Heap:       ${S1_HEAP_BASE_MB}MB (recovery) → ${S1_HEAP_FINAL_MB}MB${NC}"
echo -e "  ${CYAN}Goroutines: ${S1_GOR_PRE} → ${S1_GOR_FINAL}${NC}"

S1_HEAP_FINAL_INT=$(echo "$S1_HEAP_FINAL" | awk '{printf "%d", $1+0}')
check "server1 heap reasonable (<100MB, actual=${S1_HEAP_FINAL_MB}MB)" \
    "$([ "$S1_HEAP_FINAL_INT" -lt 104857600 ] && echo true || echo false)"

S1_GOR_LIMIT=$(( S1_GOR_PRE * 2 + 10 ))
check "server1 no goroutine leak (${S1_GOR_FINAL} <= ${S1_GOR_LIMIT})" \
    "$([ "$S1_GOR_FINAL" -le "$S1_GOR_LIMIT" ] && echo true || echo false)"

# --- Server2 Memory Analysis ---
echo ""
echo -e "  ${CYAN}--- Server2 Memory Analysis ---${NC}"
S2_HEAP_PRE=$(get_saved "server2" "pre" "heap")
S2_HEAP_FINAL=$(get_saved "server2" "final" "heap")
S2_GOR_PRE=$(get_saved "server2" "pre" "goroutines")
S2_GOR_FINAL=$(get_saved "server2" "final" "goroutines")

S2_HEAP_PRE_MB=$(echo "$S2_HEAP_PRE" | awk '{printf "%.2f", $1/1048576}')
S2_HEAP_FINAL_MB=$(echo "$S2_HEAP_FINAL" | awk '{printf "%.2f", $1/1048576}')

echo -e "  ${CYAN}Heap:       ${S2_HEAP_PRE_MB}MB → ${S2_HEAP_FINAL_MB}MB${NC}"
echo -e "  ${CYAN}Goroutines: ${S2_GOR_PRE} → ${S2_GOR_FINAL}${NC}"

S2_HEAP_FINAL_INT=$(echo "$S2_HEAP_FINAL" | awk '{printf "%d", $1+0}')
check "server2 heap reasonable (<100MB, actual=${S2_HEAP_FINAL_MB}MB)" \
    "$([ "$S2_HEAP_FINAL_INT" -lt 104857600 ] && echo true || echo false)"

S2_GOR_LIMIT=$(( S2_GOR_PRE * 2 + 10 ))
check "server2 no goroutine leak (${S2_GOR_FINAL} <= ${S2_GOR_LIMIT})" \
    "$([ "$S2_GOR_FINAL" -le "$S2_GOR_LIMIT" ] && echo true || echo false)"

# ============================================================
# Final Summary
# ============================================================
echo ""
echo -e "${BLUE}===========================================================${NC}"
echo -e "${BLUE}  Endurance Test Results: ${CHECKS_PASSED}/${CHECKS_TOTAL} checks passed${NC}"
echo -e "${BLUE}===========================================================${NC}"

if [ "$CHECKS_PASSED" -eq "$CHECKS_TOTAL" ]; then
    echo -e "${GREEN}✓ Endurance test PASSED!${NC}"
    exit 0
elif [ "$CHECKS_PASSED" -ge $((CHECKS_TOTAL * 3 / 4)) ]; then
    echo -e "${YELLOW}⚠ Endurance test PARTIALLY PASSED (${CHECKS_PASSED}/${CHECKS_TOTAL})${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    echo -e "${RED}✗ Endurance test FAILED (${CHECKS_PASSED}/${CHECKS_TOTAL})${NC}"
    exit 1
fi
