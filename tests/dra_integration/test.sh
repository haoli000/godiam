#!/bin/bash
# Integration test script
# Tests message routing from godiam peer2 through godiam dra2 to freeDiameter dra1
# and then to freeDiameter peer11 or peer12

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
TIMEOUT=${TIMEOUT:-30}
WAIT_FOR_CONNECTIONS=${WAIT_FOR_CONNECTIONS:-10}

log() {
    echo -e "$@"
}

check_process() {
    local pid=$1
    local name=$2
    if ! kill -0 "$pid" 2>/dev/null; then
        log "${RED}✗ $name (PID $pid) has died!${NC}"
        return 1
    fi
    return 0
}

log ""
log "${BLUE}=====================================${NC}"
log "${BLUE}  Integration Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

# Wait for connections to establish
log "${YELLOW}Waiting ${WAIT_FOR_CONNECTIONS}s for all peer connections...${NC}"
sleep "$WAIT_FOR_CONNECTIONS"

# Check all processes are still running
log ""
log "${BLUE}[1/4] Verifying all processes are alive...${NC}"
log "DEBUG: PID_DRA1=$PID_DRA1, PID_PEER11=$PID_PEER11, PID_PEER12=$PID_PEER12, PID_DRA2=$PID_DRA2, PID_PEER2=$PID_PEER2"
PROCESSES_OK=true
check_process "$PID_DRA1" "freeDiameter dra1" || PROCESSES_OK=false
check_process "$PID_PEER11" "freeDiameter peer11" || PROCESSES_OK=false
check_process "$PID_PEER12" "freeDiameter peer12" || PROCESSES_OK=false
check_process "$PID_DRA2" "godiam dra2" || PROCESSES_OK=false
check_process "$PID_PEER2" "godiam peer2" || PROCESSES_OK=false

if [ "$PROCESSES_OK" = false ]; then
    log "${RED}✗ Some processes have died. Check logs in /tmp/*.log${NC}"
    exit 1
fi
log "${GREEN}✓ All processes are running${NC}"

# Check peer connections in logs
log ""
log "${BLUE}[2/4] Checking peer connections...${NC}"

# Check freeDiameter dra1 connections
# freeDiameter logs state changes like: "'STATE_CLOSED' -> 'STATE_OPEN' 'dra2.backend.realm'"
if grep -q "STATE_OPEN.*peer11" /tmp/dra1.log 2>/dev/null && \
   grep -q "STATE_OPEN.*peer12" /tmp/dra1.log 2>/dev/null && \
   grep -q "STATE_OPEN.*dra2" /tmp/dra1.log 2>/dev/null; then
    log "${GREEN}✓ freeDiameter dra1 connected to peer11, peer12, and dra2${NC}"
else
    log "${YELLOW}⚠ freeDiameter dra1 connections not fully established (checking godiam logs)${NC}"
fi

# Check godiam dra2 connections (different log format)
# godiam logs:
#   "Connected to configured peer: <identity>" when outbound connection succeeds
#   "Dynamically added peer: <identity>" when accepting unknown inbound connection
#   "RCV from <identity>:" when receiving messages from a peer (proves connection is working)
DRA2_PEER2_CONNECTED=false
DRA2_DRA1_CONNECTED=false
if grep -q "Connected to configured peer: peer2\|Dynamically added peer: peer2\|RCV from peer2" /tmp/dra2.log 2>/dev/null; then
    DRA2_PEER2_CONNECTED=true
fi
if grep -q "Connected to configured peer: dra1\|Dynamically added peer: dra1\|RCV from dra1" /tmp/dra2.log 2>/dev/null; then
    DRA2_DRA1_CONNECTED=true
fi

if [ "$DRA2_PEER2_CONNECTED" = true ] && [ "$DRA2_DRA1_CONNECTED" = true ]; then
    log "${GREEN}✓ godiam dra2 connected to peer2 and dra1${NC}"
else
    log "${YELLOW}⚠ godiam dra2 connections: peer2=$DRA2_PEER2_CONNECTED dra1=$DRA2_DRA1_CONNECTED${NC}"
fi

# Wait a bit more for test message to propagate
log ""
log "${BLUE}[3/4] Waiting for test message propagation...${NC}"
sleep 5

# Check for test message routing
log ""
log "${BLUE}[4/4] Verifying message routing...${NC}"

# godiam peer2 should send test message (check for Test-Request or similar in logs)
PEER2_SENT=false
if grep -i -q "test_app send\|test.*request\|sending.*test\|test.*message" /tmp/peer2.log 2>/dev/null; then
    PEER2_SENT=true
    log "${GREEN}✓ godiam peer2 sent test message${NC}"
else
    log "${YELLOW}⚠ No clear indication of test message sent by peer2${NC}"
fi

# godiam dra2 should receive and forward (with Destination-Host dropped)
DRA2_RECEIVED=false
DRA2_REWRITE=false
if grep -i -q "RCV from peer2\|received.*request\|handling.*request" /tmp/dra2.log 2>/dev/null; then
    DRA2_RECEIVED=true
    log "${GREEN}✓ godiam dra2 received message from peer2${NC}"
fi
if grep -i -q "Dropped.*Destination-Host\|drop.*destination-host\|rewrite.*destination" /tmp/dra2.log 2>/dev/null; then
    DRA2_REWRITE=true
    log "${GREEN}✓ godiam dra2 applied rt_rewrite (dropped Destination-Host)${NC}"
else
    log "${YELLOW}⚠ No clear indication of rt_rewrite in dra2 logs${NC}"
fi

# freeDiameter dra1 should receive and route to peer11 or peer12
DRA1_ROUTED=false
# freeDiameter logs routing or we see answer from peer11/12 in dra2 logs
if grep -i -q "routing.*peer11\|routing.*peer12\|forwarding.*peer1[12]" /tmp/dra1.log 2>/dev/null; then
    DRA1_ROUTED=true
    log "${GREEN}✓ freeDiameter dra1 routed message to peer11/peer12${NC}"
elif grep -i -q "RCV from dra1\|Origin-Host: peer1" /tmp/dra2.log 2>/dev/null; then
    # If dra2 received answer from dra1 with Origin-Host from peer11/peer12, routing worked
    DRA1_ROUTED=true
    log "${GREEN}✓ freeDiameter dra1 routed message to peer11/peer12 (answer received)${NC}"
else
    log "${YELLOW}⚠ No clear indication of routing in dra1 logs${NC}"
fi

# Check if peer11 or peer12 received and answered (via answer in peer2 logs)
PEER_ANSWERED=false
# Check peer2 log for answer with Origin-Host from peer11 or peer12
if grep -q "Origin-Host: peer11\|Origin-Host: peer12" /tmp/peer2.log 2>/dev/null; then
    PEER_ANSWERED=true
    log "${GREEN}✓ Answer received from freeDiameter peer11/peer12${NC}"
elif grep -q "result: 3001\|result: 2001" /tmp/peer2.log 2>/dev/null; then
    # Result code received (3001=DIAMETER_COMMAND_UNSUPPORTED is OK, 2001=success)
    PEER_ANSWERED=true
    log "${GREEN}✓ Answer received by godiam peer2 (result code found)${NC}"
elif grep -i -q "test_app answer" /tmp/peer2.log 2>/dev/null; then
    PEER_ANSWERED=true
    log "${GREEN}✓ godiam peer2 received answer${NC}"
else
    log "${YELLOW}⚠ No clear indication of answer received by peer2${NC}"
fi

# Final summary
log ""
log "${BLUE}=====================================${NC}"
log "${BLUE}  Test Summary${NC}"
log "${BLUE}=====================================${NC}"
log ""

SCORE=0
TOTAL=5

[ "$PROCESSES_OK" = true ] && SCORE=$((SCORE + 1))
[ "$PEER2_SENT" = true ] && SCORE=$((SCORE + 1))
[ "$DRA2_REWRITE" = true ] && SCORE=$((SCORE + 1))
[ "$DRA1_ROUTED" = true ] && SCORE=$((SCORE + 1))
[ "$PEER_ANSWERED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ Integration test PASSED!${NC}"
    log "${GREEN}✓ freeDiameter and godiam interoperate successfully${NC}"
    exit 0
elif [ $SCORE -ge 3 ]; then
    log "${YELLOW}⚠ Integration test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    log "${YELLOW}⚠ Basic functionality works but some checks failed${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    log "${RED}✗ Integration test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
