#!/bin/bash
# Test validation for rt_deny_by_size
# Validates that oversized messages are rejected

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
log "${BLUE}  rt_deny_by_size Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

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

# Check 2: Client sent test messages
log ""
log "${BLUE}[2/4] Checking if client sent test messages...${NC}"
CLIENT_SENT=false
if grep -qE "test_app send" /tmp/client.log 2>/dev/null; then
    CLIENT_SENT=true
    log "${GREEN}✓ Client sent test messages${NC}"
else
    log "${YELLOW}⚠ No test messages sent by client${NC}"
fi

# Check 3: rt_deny_by_size initialized
log ""
log "${BLUE}[3/4] Checking if rt_deny_by_size initialized...${NC}"
DENY_INIT=false
if grep -q "Initializing extension: rt_deny_by_size" /tmp/gw.log 2>/dev/null; then
    DENY_INIT=true
    log "${GREEN}✓ rt_deny_by_size extension initialized (max_size=100)${NC}"
else
    log "${YELLOW}⚠ rt_deny_by_size initialization not found${NC}"
fi

# Check 4: Messages were rejected by size limit
log ""
log "${BLUE}[4/4] Checking if messages were rejected by size...${NC}"
REJECTED=false
if grep -q "rt_deny_by_size: rejecting" /tmp/gw.log 2>/dev/null; then
    REJECT_COUNT=$(grep -c "rt_deny_by_size: rejecting" /tmp/gw.log 2>/dev/null || true)
    REJECTED=true
    log "${GREEN}✓ ${REJECT_COUNT} message(s) rejected by size limit${NC}"
    SAMPLE=$(grep "rt_deny_by_size: rejecting" /tmp/gw.log 2>/dev/null | head -1)
    log "  Sample: ${SAMPLE}"
else
    log "${YELLOW}⚠ No messages rejected by size limit${NC}"
    log "${YELLOW}  This may indicate messages are smaller than 100 bytes${NC}"
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
[ "$CLIENT_SENT" = true ] && SCORE=$((SCORE + 1))
[ "$DENY_INIT" = true ] && SCORE=$((SCORE + 1))
[ "$REJECTED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ rt_deny_by_size test PASSED!${NC}"
    log "${GREEN}✓ Message size enforcement working correctly${NC}"
    exit 0
elif [ $SCORE -ge 2 ]; then
    log "${YELLOW}⚠ rt_deny_by_size test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    # A partial pass is a failure: exiting 0 here once let a
    # completely non-functional extension report green for months.
    exit 1
else
    log "${RED}✗ rt_deny_by_size test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
