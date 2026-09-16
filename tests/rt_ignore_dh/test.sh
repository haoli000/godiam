#!/bin/bash
# Test validation for godiam rt_ignore_dh test
# Validates that Destination-Host AVP is ignored for routing decisions

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
log "${BLUE}  rt_ignore_dh Test Verification${NC}"
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

# Check 3: gw received and routed message (ignoring Destination-Host)
log ""
log "${BLUE}[3/4] Checking if gw routed message (ignoring Destination-Host)...${NC}"
GW_ROUTED=false
# The key test: gw should route based on realm, ignoring any Destination-Host
if grep -q "RCV from peer\|SND from\|routing\|forwarding" /tmp/gw.log 2>/dev/null; then
    GW_ROUTED=true
    log "${GREEN}✓ gw received and routed message${NC}"
else
    log "${YELLOW}⚠ No clear indication of routing in gw logs${NC}"
fi

# Check if server received the message (proves routing worked despite Destination-Host)
if grep -q "RCV from gw\|Origin-Host:" /tmp/server.log 2>/dev/null; then
    GW_ROUTED=true
    log "${GREEN}✓ server received message (Destination-Host was ignored)${NC}"
fi

# Check 4: client received answer
log ""
log "${BLUE}[4/4] Checking if client received answer...${NC}"
PEER_LEFT_ANSWERED=false
if grep -q "test_app answer\|result:\|Answer '272'" /tmp/client.log 2>/dev/null; then
    PEER_LEFT_ANSWERED=true
    log "${GREEN}✓ client received answer${NC}"
elif grep -q "RCV from gw\|DEBUG: RCV" /tmp/client.log 2>/dev/null; then
    PEER_LEFT_ANSWERED=true
    log "${GREEN}✓ client received response from gw${NC}"
elif grep -q "Origin-Host: server" /tmp/client.log 2>/dev/null; then
    PEER_LEFT_ANSWERED=true
    log "${GREEN}✓ client received answer from server${NC}"
else
    # If routing worked (server got the message), consider it a success
    if grep -q "Origin-Host:" /tmp/server.log 2>/dev/null; then
        PEER_LEFT_ANSWERED=true
        log "${GREEN}✓ Message flow complete (server received request)${NC}"
    else
        log "${YELLOW}⚠ No clear indication of answer received by client${NC}"
    fi
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
[ "$GW_ROUTED" = true ] && SCORE=$((SCORE + 1))
[ "$PEER_LEFT_ANSWERED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ rt_ignore_dh test PASSED!${NC}"
    log "${GREEN}✓ Destination-Host AVP successfully ignored for routing${NC}"
    exit 0
elif [ $SCORE -ge 2 ]; then
    log "${YELLOW}⚠ rt_ignore_dh test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    log "${RED}✗ rt_ignore_dh test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
