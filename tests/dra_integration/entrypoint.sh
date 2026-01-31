#!/bin/bash
# Entrypoint script for freeDiameter + godiam integration test
# Left side: freeDiameter (dra1, peer11, peer12)
# Right side: godiam (dra2, peer2)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "=============================================="
echo "  freeDiameter + godiam Integration Test"
echo "  Left: freeDiameter | Right: godiam"
echo "=============================================="
echo ""

# Check if manual mode
MANUAL_MODE=false
if [ "$1" = "--manual" ] || [ "$1" = "-m" ]; then
    MANUAL_MODE=true
    echo -e "${YELLOW}Manual mode enabled. Services will start and wait for your commands.${NC}"
    echo ""
fi

# Set up log file
LOGFILE="/tmp/integration_test.log"
echo "" > "$LOGFILE"

log() {
    echo -e "$@" | tee -a "$LOGFILE"
}

# Cleanup function
cleanup() {
    log ""
    log "${YELLOW}Cleaning up processes...${NC}"
    pkill -f freeDiameterd 2>/dev/null || true
    pkill -f godiam-diameterd 2>/dev/null || true
}
trap cleanup EXIT

# Start all instances
log ""
log "${BLUE}[1/5] Starting freeDiameter dra1 (left side)...${NC}"
/usr/local/bin/freeDiameterd -c /conf/dra1.conf -dd > /tmp/dra1.log 2>&1 &
PID_DRA1=$!
sleep 2

log "${BLUE}[2/5] Starting freeDiameter peer11 (left side)...${NC}"
/usr/local/bin/freeDiameterd -c /conf/peer11.conf -dd > /tmp/peer11.log 2>&1 &
PID_PEER11=$!
sleep 2

log "${BLUE}[3/5] Starting freeDiameter peer12 (left side)...${NC}"
/usr/local/bin/freeDiameterd -c /conf/peer12.conf -dd > /tmp/peer12.log 2>&1 &
PID_PEER12=$!
sleep 2

log "${BLUE}[4/5] Starting godiam dra2 (right side)...${NC}"
/usr/local/bin/godiam-diameterd --config /conf/dra2.yaml > /tmp/dra2.log 2>&1 &
PID_DRA2=$!
sleep 2

log "${BLUE}[5/5] Starting godiam peer2 (right side)...${NC}"
/usr/local/bin/godiam-diameterd --config /conf/peer2.yaml > /tmp/peer2.log 2>&1 &
PID_PEER2=$!
sleep 2

log ""
log "${GREEN}All services started:${NC}"
log "  freeDiameter dra1:   PID $PID_DRA1"
log "  freeDiameter peer11: PID $PID_PEER11"
log "  freeDiameter peer12: PID $PID_PEER12"
log "  godiam dra2:         PID $PID_DRA2"
log "  godiam peer2:        PID $PID_PEER2"
log ""

# Export PIDs for test script
export PID_DRA1 PID_PEER11 PID_PEER12 PID_DRA2 PID_PEER2

# If manual mode, keep container running
if [ "$MANUAL_MODE" = true ]; then
    log "${YELLOW}Manual mode: Container will stay running.${NC}"
    log "Logs are available in /tmp/*.log"
    log "Use 'docker exec' to interact with the container."
    log ""
    # Tail logs
    tail -f /tmp/*.log
else
    # Run automated test
    log "${BLUE}Running automated test...${NC}"
    /test.sh
fi
