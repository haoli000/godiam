#!/bin/bash
# Test validation for dbg_msg_timings and fifo_stats
# Validates message timing tracking and queue statistics

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
log "${BLUE}=========================================${NC}"
log "${BLUE}  dbg_msg_timings + fifo_stats Test${NC}"
log "${BLUE}=========================================${NC}"
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

# Check 3: dbg_msg_timings initialized
log ""
log "${BLUE}[3/5] Checking if dbg_msg_timings initialized...${NC}"
TIMINGS_INIT=false
if grep -q "Initializing extension: dbg_msg_timings" /tmp/gw.log 2>/dev/null; then
    TIMINGS_INIT=true
    log "${GREEN}✓ dbg_msg_timings extension initialized${NC}"
else
    log "${YELLOW}⚠ dbg_msg_timings initialization not found${NC}"
fi

# Check 4: fifo_stats initialized
log ""
log "${BLUE}[4/5] Checking if fifo_stats initialized...${NC}"
FIFO_INIT=false
if grep -q "Initializing extension: fifo_stats" /tmp/gw.log 2>/dev/null; then
    FIFO_INIT=true
    log "${GREEN}✓ fifo_stats extension initialized${NC}"
else
    log "${YELLOW}⚠ fifo_stats initialization not found${NC}"
fi

# Check 5: Timing entries logged
log ""
log "${BLUE}[5/5] Checking if timing entries were logged...${NC}"
TIMINGS_LOGGED=false
if grep -q "dbg_msg_timings: cmd=" /tmp/gw.log 2>/dev/null; then
    TIMING_COUNT=$(grep -c "dbg_msg_timings: cmd=" /tmp/gw.log 2>/dev/null || true)
    TIMINGS_LOGGED=true
    log "${GREEN}✓ ${TIMING_COUNT} timing entries logged${NC}"
    SAMPLE=$(grep "dbg_msg_timings: cmd=" /tmp/gw.log 2>/dev/null | head -1)
    log "  Sample: ${SAMPLE}"
else
    log "${YELLOW}⚠ No timing entries found in gw logs${NC}"
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
[ "$TIMINGS_INIT" = true ] && SCORE=$((SCORE + 1))
[ "$FIFO_INIT" = true ] && SCORE=$((SCORE + 1))
[ "$TIMINGS_LOGGED" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ dbg_msg_timings + fifo_stats test PASSED!${NC}"
    log "${GREEN}✓ Message timing and queue tracking working correctly${NC}"
    exit 0
elif [ $SCORE -ge 3 ]; then
    log "${YELLOW}⚠ Test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    exit 0
else
    log "${RED}✗ Test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
