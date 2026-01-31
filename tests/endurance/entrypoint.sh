#!/bin/bash
# Entrypoint for godiam endurance test
# Orchestrates multi-phase production scenario testing

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configurable parameters
PHASE_DURATION=${PHASE_DURATION:-20}
BENCH_RATE=${BENCH_RATE:-500}
BENCH_CONN=${BENCH_CONN:-10}
BURST_RATE=${BURST_RATE:-3000}

echo -e "${BLUE}===========================================================${NC}"
echo -e "${BLUE}  godiam Endurance Test${NC}"
echo -e "${BLUE}  Production scenario simulation with DRA + extensions${NC}"
echo -e "${BLUE}===========================================================${NC}"
echo ""
echo -e "  Phase duration: ${PHASE_DURATION}s"
echo -e "  Base rate:      ${BENCH_RATE} req/s"
echo -e "  Connections:    ${BENCH_CONN}"
echo -e "  Burst rate:     ${BURST_RATE} req/s"
echo ""

# --- Start services ---
echo -e "${BLUE}[Setup] Starting backend services...${NC}"

echo -e "${CYAN}  Starting server1 (port 3870)...${NC}"
/usr/local/bin/diameterd --config /conf/server1.yaml > /tmp/server1.log 2>&1 &
PID_SERVER1=$!
sleep 1

if ! kill -0 $PID_SERVER1 2>/dev/null; then
    echo -e "${RED}  server1 failed to start:${NC}"
    cat /tmp/server1.log
    exit 1
fi
echo -e "${GREEN}  server1 started (PID $PID_SERVER1)${NC}"

echo -e "${CYAN}  Starting server2 (port 3871)...${NC}"
/usr/local/bin/diameterd --config /conf/server2.yaml > /tmp/server2.log 2>&1 &
PID_SERVER2=$!
sleep 1

if ! kill -0 $PID_SERVER2 2>/dev/null; then
    echo -e "${RED}  server2 failed to start:${NC}"
    cat /tmp/server2.log
    exit 1
fi
echo -e "${GREEN}  server2 started (PID $PID_SERVER2)${NC}"

echo -e "${CYAN}  Starting DRA (port 3869)...${NC}"
/usr/local/bin/diameterd --config /conf/dra.yaml > /tmp/dra.log 2>&1 &
PID_DRA=$!
sleep 2

if ! kill -0 $PID_DRA 2>/dev/null; then
    echo -e "${RED}  DRA failed to start:${NC}"
    cat /tmp/dra.log
    kill $PID_SERVER1 $PID_SERVER2 2>/dev/null || true
    exit 1
fi
echo -e "${GREEN}  DRA started (PID $PID_DRA)${NC}"

# Wait for peer connections
echo -e "${CYAN}  Waiting for peer connections...${NC}"
sleep 3

echo -e "${GREEN}[Setup] All services running${NC}"
echo ""

# Export for test.sh
export PID_SERVER1 PID_SERVER2 PID_DRA
export PHASE_DURATION BENCH_RATE BENCH_CONN BURST_RATE

if [ "$1" = "manual" ]; then
    echo "Manual mode: streaming logs..."
    echo "Press Ctrl+C to exit"
    echo ""
    tail -f /tmp/dra.log | sed 's/^/[dra] /' &
    tail -f /tmp/server1.log | sed 's/^/[server1] /' &
    tail -f /tmp/server2.log | sed 's/^/[server2] /' &
    wait
elif [ "$1" = "bash" ]; then
    exec /bin/bash
else
    /test.sh
    TEST_RESULT=$?

    echo ""
    echo -e "${BLUE}Cleaning up...${NC}"
    # Clean up iptables just in case
    iptables -D OUTPUT -p tcp --dport 3871 -j DROP 2>/dev/null || true
    kill $PID_DRA $PID_SERVER1 $PID_SERVER2 2>/dev/null || true
    wait $PID_DRA $PID_SERVER1 $PID_SERVER2 2>/dev/null || true

    exit $TEST_RESULT
fi
