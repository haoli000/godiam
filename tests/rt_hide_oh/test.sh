#!/bin/bash
# Test validation for godiam rt_hide_oh test
# Validates that Origin-Host AVP is replaced by gateway's identity

set -e

# Colors
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
log "${BLUE}  rt_hide_oh Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

# Wait for connections and test messages
log "Waiting 8s for peer connections and test messages..."
sleep 8

# Check 1: All processes running
log ""
log "${BLUE}[1/4] Verifying all processes are alive...${NC}"
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

# Check 2: client sent test message
log ""
log "${BLUE}[2/4] Checking if client sent test message...${NC}"
PEER_LEFT_SENT=false
if grep -q "test_app send\|Test-Request\|SND from" /tmp/client.log 2>/dev/null; then
    PEER_LEFT_SENT=true
    log "${GREEN}✓ client sent test message${NC}"
else
    log "${YELLOW}⚠ No clear indication of test message sent by client${NC}"
fi

# Check 3: server received message with Origin-Host replaced by gw
log ""
log "${BLUE}[3/4] Checking if Origin-Host was hidden (replaced by gw identity)...${NC}"
OH_HIDDEN=false
# The key test: server should see Origin-Host: gw.backend.realm (not client.access.realm)
if grep -q "Origin-Host: gw.backend.realm" /tmp/server.log 2>/dev/null; then
    OH_HIDDEN=true
    log "${GREEN}✓ Origin-Host replaced with gateway identity (gw.backend.realm)${NC}"
elif grep -q "Origin-Host:" /tmp/server.log 2>/dev/null; then
    # Check if it's NOT the original peer
    if ! grep -q "Origin-Host: client.access.realm" /tmp/server.log 2>/dev/null; then
        OH_HIDDEN=true
        log "${GREEN}✓ Origin-Host was hidden (not client.access.realm)${NC}"
    else
        log "${RED}✗ Origin-Host NOT hidden - still shows client.access.realm${NC}"
    fi
else
    log "${YELLOW}⚠ No Origin-Host found in server logs${NC}"
fi

# Check 4: client received answer
log ""
log "${BLUE}[4/4] Checking if client received answer...${NC}"
PEER_LEFT_ANSWERED=false
if grep -q "test_app answer\|result:\|Answer '272'" /tmp/client.log 2>/dev/null; then
    PEER_LEFT_ANSWERED=true
    log "${GREEN}✓ client received answer${NC}"
elif grep -q "RCV from gw" /tmp/client.log 2>/dev/null; then
    PEER_LEFT_ANSWERED=true
    log "${GREEN}✓ client received response from gw${NC}"
else
    log "${YELLOW}⚠ No clear indication of answer received by client${NC}"
fi

# Summary
log ""
log "${BLUE}=====================================${NC}"
log "${BLUE}  Test Summary${NC}"
log "${BLUE}=====================================${NC}"
log ""

SCORE=0
TOTAL=4

[ "$PROCESSES_OK" = true ] && SCORE=$((SCORE + 1))
[ "$PEER_LEFT_SENT" = true ] && SCORE=$((SCORE + 1))
[ "$OH_HIDDEN" = true ] && SCORE=$((SCORE + 1))
[ "$PEER_LEFT_ANSWERED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ rt_hide_oh test PASSED!${NC}"
    log "${GREEN}✓ Origin-Host AVP successfully hidden${NC}"
    exit 0
elif [ $SCORE -ge 2 ]; then
    log "${YELLOW}⚠ rt_hide_oh test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    log "${RED}✗ rt_hide_oh test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
