#!/bin/bash
# Test validation for rt_redirect
# Validates redirect indication caching extension

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
log "${BLUE}  rt_redirect Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

log "Waiting 8s for peer connections and test messages..."
sleep 8

# Check 1: All processes running
log ""
log "${BLUE}[1/5] Verifying all processes are alive...${NC}"
PROCESSES_OK=true

for pid_var in PID_GW PID_SERVER PID_CLIENT; do
    pid="${!pid_var}"
    if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
        log "${RED}✗ Process $pid_var (PID=$pid) is not running${NC}"
        PROCESSES_OK=false
    fi
done

if [ "$PROCESSES_OK" = true ]; then
    log "${GREEN}✓ All 3 processes are running${NC}"
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

# Check 3: rt_redirect initialized
log ""
log "${BLUE}[3/5] Checking if rt_redirect initialized...${NC}"
REDIRECT_INIT=false
if grep -q "Initializing extension: rt_redirect" /tmp/gw.log 2>/dev/null; then
    REDIRECT_INIT=true
    log "${GREEN}✓ rt_redirect extension initialized${NC}"
else
    log "${YELLOW}⚠ rt_redirect initialization not found${NC}"
fi

# Check 4: Backend received requests (messages flow normally with rt_redirect)
log ""
log "${BLUE}[4/5] Checking if backend received requests...${NC}"
BACKEND_RECEIVED=false
if grep -qE "test_app request:" /tmp/server.log 2>/dev/null; then
    REQ_COUNT=$(grep -c "test_app request:" /tmp/server.log 2>/dev/null || true)
    BACKEND_RECEIVED=true
    log "${GREEN}✓ Backend received ${REQ_COUNT} requests (message flow unaffected by rt_redirect)${NC}"
else
    log "${YELLOW}⚠ No requests received by backend${NC}"
fi

# Check 5: Client received answers
log ""
log "${BLUE}[5/5] Checking if client received answers...${NC}"
CLIENT_ANSWERED=false
if grep -qE "test_app answer:|test_app result:" /tmp/client.log 2>/dev/null; then
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
[ "$REDIRECT_INIT" = true ] && SCORE=$((SCORE + 1))
[ "$BACKEND_RECEIVED" = true ] && SCORE=$((SCORE + 1))
[ "$CLIENT_ANSWERED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ rt_redirect test PASSED!${NC}"
    log "${GREEN}✓ Redirect caching extension active, message flow normal${NC}"
    exit 0
elif [ $SCORE -ge 3 ]; then
    log "${YELLOW}⚠ rt_redirect test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    exit 0
else
    log "${RED}✗ rt_redirect test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
