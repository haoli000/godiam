#!/bin/bash
# Integration test validation for godiam-only DRA test
# Validates message flow: peer2 -> dra2 -> dra1 -> peer11/peer12 and back

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
log "${BLUE}  godiam DRA Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

# Wait for connections and test messages
log "Waiting 8s for all peer connections and test messages..."
sleep 8

# Check 1: All processes running
log ""
log "${BLUE}[1/5] Verifying all processes are alive...${NC}"
PROCESSES_OK=true

for pid_var in PID_DRA1 PID_PEER11 PID_PEER12 PID_DRA2 PID_PEER2; do
    pid="${!pid_var}"
    if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
        log "${RED}✗ Process $pid_var (PID=$pid) is not running${NC}"
        PROCESSES_OK=false
    fi
done

if [ "$PROCESSES_OK" = true ]; then
    log "${GREEN}✓ All 5 processes are running${NC}"
fi

# Check 2: peer2 sent test message
log ""
log "${BLUE}[2/5] Checking if peer2 sent test message...${NC}"
PEER2_SENT=false
if grep -q "test_app send\|Test-Request\|sending.*test" /tmp/peer2.log 2>/dev/null; then
    PEER2_SENT=true
    log "${GREEN}✓ godiam peer2 sent test message${NC}"
else
    log "${YELLOW}⚠ No clear indication of test message sent by peer2${NC}"
fi

# Check 3: dra2 received and forwarded message
log ""
log "${BLUE}[3/5] Checking if dra2 received and forwarded message...${NC}"
DRA2_FORWARDED=false
if grep -q "RCV from peer2\|SND from\|forwarding\|routing" /tmp/dra2.log 2>/dev/null; then
    DRA2_FORWARDED=true
    log "${GREEN}✓ godiam dra2 received and forwarded message${NC}"
else
    log "${YELLOW}⚠ No clear indication of message forwarding in dra2 logs${NC}"
fi

# Check 4: dra1 received and routed to peer11/peer12
log ""
log "${BLUE}[4/5] Checking if dra1 routed message to peer11/peer12...${NC}"
DRA1_ROUTED=false
if grep -q "RCV from dra2\|SND from\|routing\|forwarding" /tmp/dra1.log 2>/dev/null; then
    DRA1_ROUTED=true
    log "${GREEN}✓ godiam dra1 received and routed message${NC}"
elif grep -q "Origin-Host: peer11\|Origin-Host: peer12" /tmp/dra2.log 2>/dev/null; then
    # If dra2 received answer from peer11/peer12, dra1 routing worked
    DRA1_ROUTED=true
    log "${GREEN}✓ godiam dra1 routed message (answer received)${NC}"
else
    log "${YELLOW}⚠ No clear indication of routing in dra1 logs${NC}"
fi

# Check 5: peer2 received answer
log ""
log "${BLUE}[5/5] Checking if peer2 received answer...${NC}"
PEER2_ANSWERED=false
if grep -q "Origin-Host: peer11\|Origin-Host: peer12" /tmp/peer2.log 2>/dev/null; then
    PEER2_ANSWERED=true
    log "${GREEN}✓ Answer received from peer11/peer12${NC}"
elif grep -q "test_app answer\|result:" /tmp/peer2.log 2>/dev/null; then
    PEER2_ANSWERED=true
    log "${GREEN}✓ Answer received by peer2${NC}"
elif grep -q "Answer '272'" /tmp/peer2.log 2>/dev/null; then
    PEER2_ANSWERED=true
    log "${GREEN}✓ Answer (272) received by peer2${NC}"
else
    log "${YELLOW}⚠ No clear indication of answer received by peer2${NC}"
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
[ "$PEER2_SENT" = true ] && SCORE=$((SCORE + 1))
[ "$DRA2_FORWARDED" = true ] && SCORE=$((SCORE + 1))
[ "$DRA1_ROUTED" = true ] && SCORE=$((SCORE + 1))
[ "$PEER2_ANSWERED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ DRA test PASSED!${NC}"
    log "${GREEN}✓ All godiam instances interoperate successfully${NC}"
    exit 0
elif [ $SCORE -ge 3 ]; then
    log "${YELLOW}⚠ DRA test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    log "${YELLOW}⚠ Basic functionality works but some checks failed${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    log "${RED}✗ DRA test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
