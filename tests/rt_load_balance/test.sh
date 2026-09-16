#!/bin/bash
# Test validation for rt_load_balance
# Validates that requests are distributed across multiple backend peers

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
    echo -e "$1"
}

log ""
log "${BLUE}=====================================${NC}"
log "${BLUE}  rt_load_balance Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

log "Waiting 12s for peer connections and test messages..."
sleep 12

# Check 1: All processes running
log ""
log "${BLUE}[1/5] Verifying all processes are alive...${NC}"
PROCESSES_OK=true

for pid_var in PID_GW PID_SERVER1 PID_SERVER2 PID_CLIENT; do
    pid="${!pid_var}"
    if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
        log "${RED}✗ Process $pid_var (PID=$pid) is not running${NC}"
        PROCESSES_OK=false
    fi
done

if [ "$PROCESSES_OK" = true ]; then
    log "${GREEN}✓ All 4 processes are running${NC}"
fi

# Check 2: Client sent test messages
log ""
log "${BLUE}[2/5] Checking if client sent test messages...${NC}"
CLIENT_SENT=false
if grep -qE "test_app send" /tmp/client.log 2>/dev/null; then
    SEND_COUNT=$(grep -c "test_app send request:" /tmp/client.log 2>/dev/null || true)
    CLIENT_SENT=true
    log "${GREEN}✓ Client sent ${SEND_COUNT} test messages${NC}"
else
    log "${YELLOW}⚠ No test messages sent by client${NC}"
fi

# Check 3: rt_load_balance initialized on gw
log ""
log "${BLUE}[3/5] Checking if rt_load_balance extension initialized...${NC}"
LB_INIT=false
if grep -q "Initializing extension: rt_load_balance" /tmp/gw.log 2>/dev/null; then
    LB_INIT=true
    log "${GREEN}✓ rt_load_balance extension initialized${NC}"
else
    log "${YELLOW}⚠ rt_load_balance initialization not found in gw logs${NC}"
fi

# Check 4: Both backends received requests (load distributed)
log ""
log "${BLUE}[4/5] Checking load distribution across backends...${NC}"
LOAD_DISTRIBUTED=false
BACKEND1_COUNT=$(grep -c "test_app request:" /tmp/server1.log 2>/dev/null || true)
BACKEND1_COUNT=${BACKEND1_COUNT:-0}
BACKEND2_COUNT=$(grep -c "test_app request:" /tmp/server2.log 2>/dev/null || true)
BACKEND2_COUNT=${BACKEND2_COUNT:-0}

if [ "$BACKEND1_COUNT" -gt 0 ] && [ "$BACKEND2_COUNT" -gt 0 ]; then
    LOAD_DISTRIBUTED=true
    log "${GREEN}✓ Load distributed: backend1=${BACKEND1_COUNT}, backend2=${BACKEND2_COUNT}${NC}"
elif [ "$BACKEND1_COUNT" -gt 0 ] || [ "$BACKEND2_COUNT" -gt 0 ]; then
    log "${YELLOW}⚠ Only one backend received requests (backend1=${BACKEND1_COUNT}, backend2=${BACKEND2_COUNT})${NC}"
    log "${YELLOW}  Load balancing may not be effective${NC}"
else
    log "${RED}✗ No backends received any requests${NC}"
fi

# Check 5: Client received answers
log ""
log "${BLUE}[5/5] Checking if client received answers...${NC}"
CLIENT_ANSWERED=false
if grep -qE "test_app answer:|test_app result:|Answer '272'" /tmp/client.log 2>/dev/null; then
    ANS_COUNT=$(grep -c "test_app answer:" /tmp/client.log 2>/dev/null || true)
    CLIENT_ANSWERED=true
    log "${GREEN}✓ Client received ${ANS_COUNT} answers${NC}"
else
    log "${YELLOW}⚠ No answers received by client${NC}"
fi

# Summary
log ""
log "${BLUE}=====================================${NC}"
log "${BLUE}  Test Summary${NC}"
log "${BLUE}=====================================${NC}"
log ""

SCORE=0
TOTAL=5

[ "$PROCESSES_OK" = true ] && SCORE=$((SCORE + 1))
[ "$CLIENT_SENT" = true ] && SCORE=$((SCORE + 1))
[ "$LB_INIT" = true ] && SCORE=$((SCORE + 1))
[ "$LOAD_DISTRIBUTED" = true ] && SCORE=$((SCORE + 1))
[ "$CLIENT_ANSWERED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ rt_load_balance test PASSED!${NC}"
    log "${GREEN}✓ Load balancing across peers working correctly${NC}"
    exit 0
elif [ $SCORE -ge 3 ]; then
    log "${YELLOW}⚠ rt_load_balance test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    log "${RED}✗ rt_load_balance test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
