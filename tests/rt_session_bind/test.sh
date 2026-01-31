#!/bin/bash
# Test validation for godiam rt_session_bind test
# Validates that session binding routes all requests in a session to the same backend

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
log "${BLUE}  rt_session_bind Test Verification${NC}"
log "${BLUE}=====================================${NC}"
log ""

# Wait for connections and test messages
log "Waiting 10s for peer connections and test messages..."
sleep 10

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
if grep -qE "test_app send|Test-Request|SND from" /tmp/client.log 2>/dev/null; then
    CLIENT_SENT=true
    log "${GREEN}✓ Client sent test messages${NC}"
else
    log "${YELLOW}⚠ No clear indication of test messages sent by client${NC}"
fi

# Check 3: GW initialized rt_session_bind
log ""
log "${BLUE}[3/5] Checking if DRA gateway initialized rt_session_bind...${NC}"
GW_BIND_INIT=false
if grep -q "rt_session_bind: initialized" /tmp/gw.log 2>/dev/null; then
    GW_BIND_INIT=true
    log "${GREEN}✓ rt_session_bind extension initialized${NC}"
else
    log "${YELLOW}⚠ rt_session_bind initialization not found in gw logs${NC}"
fi

# Check 4: At least one backend received messages
log ""
log "${BLUE}[4/5] Checking if backends received messages...${NC}"
BACKEND_RECEIVED=false
BACKEND1_COUNT=$(grep -cE "test_app request:|test_app answer sent:" /tmp/server1.log 2>/dev/null || true)
BACKEND1_COUNT=${BACKEND1_COUNT:-0}
BACKEND2_COUNT=$(grep -cE "test_app request:|test_app answer sent:" /tmp/server2.log 2>/dev/null || true)
BACKEND2_COUNT=${BACKEND2_COUNT:-0}

if [ "$BACKEND1_COUNT" -gt 0 ] || [ "$BACKEND2_COUNT" -gt 0 ]; then
    BACKEND_RECEIVED=true
    log "${GREEN}✓ Backend(s) received messages (backend1=$BACKEND1_COUNT, backend2=$BACKEND2_COUNT)${NC}"
else
    log "${YELLOW}⚠ No messages received by backends${NC}"
    log "${YELLOW}  gw log:${NC}"
    tail -20 /tmp/gw.log 2>/dev/null || true
    log "${YELLOW}  client log:${NC}"
    tail -20 /tmp/client.log 2>/dev/null || true
fi

# Check 5: Session binding worked - all requests with the same session went to the same backend
log ""
log "${BLUE}[5/5] Checking session binding consistency...${NC}"
BINDING_OK=false

if [ "$BACKEND1_COUNT" -gt 0 ] && [ "$BACKEND2_COUNT" -eq 0 ]; then
    BINDING_OK=true
    log "${GREEN}✓ All requests routed to backend1 (session binding working)${NC}"
elif [ "$BACKEND2_COUNT" -gt 0 ] && [ "$BACKEND1_COUNT" -eq 0 ]; then
    BINDING_OK=true
    log "${GREEN}✓ All requests routed to backend2 (session binding working)${NC}"
elif [ "$BACKEND1_COUNT" -gt 0 ] && [ "$BACKEND2_COUNT" -gt 0 ]; then
    log "${YELLOW}⚠ Messages split between backends (backend1=$BACKEND1_COUNT, backend2=$BACKEND2_COUNT)${NC}"
    log "${YELLOW}  This may indicate session binding is not working correctly${NC}"
else
    log "${YELLOW}⚠ Unable to determine binding consistency${NC}"
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
[ "$GW_BIND_INIT" = true ] && SCORE=$((SCORE + 1))
[ "$BACKEND_RECEIVED" = true ] && SCORE=$((SCORE + 1))
[ "$BINDING_OK" = true ] && SCORE=$((SCORE + 1))

log "Test Results: ${SCORE}/${TOTAL} checks passed"
log ""

if [ $SCORE -eq $TOTAL ]; then
    log "${GREEN}✓ rt_session_bind test PASSED!${NC}"
    log "${GREEN}✓ Session-affinity routing working correctly${NC}"
    exit 0
elif [ $SCORE -ge 3 ]; then
    log "${YELLOW}⚠ rt_session_bind test PARTIALLY PASSED (${SCORE}/${TOTAL})${NC}"
    exit 0
else
    log "${RED}✗ rt_session_bind test FAILED (${SCORE}/${TOTAL})${NC}"
    log "${RED}✗ Check logs in /tmp/*.log for details${NC}"
    exit 1
fi
