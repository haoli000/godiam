#!/bin/bash
# Entrypoint for the Diameter Edge Agent integration test.
#
# Start order matters: the internal application node must be listening before
# the edge agent connects to it, and the partners only connect once the edge
# agent's external zone is up.

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo "=============================================="
echo "  godiam Diameter Edge Agent Integration Test"
echo "=============================================="
echo ""

MANUAL_MODE=false
if [ "$1" = "--manual" ] || [ "$1" = "-m" ]; then
    MANUAL_MODE=true
    echo -e "${YELLOW}Manual mode enabled. Nodes stay up after startup.${NC}"
    echo ""
fi

LOGFILE="/tmp/dea_integration.log"
echo "" > "$LOGFILE"

log() {
    echo -e "$@" | tee -a "$LOGFILE"
}

cleanup() {
    log ""
    log "${YELLOW}Cleaning up processes...${NC}"
    for pid in $PID_HSS $PID_DEA $PID_GOOD $PID_EVIL $PID_FLOOD; do
        kill "$pid" 2>/dev/null || true
    done
}
trap cleanup EXIT

start() {
    local name=$1 conf=$2
    /usr/local/bin/godiam-diameterd --config "/conf/$conf" > "/tmp/$name.log" 2>&1 &
    echo $!
}

log "${BLUE}[1/5] Starting internal application node (hss.core.realm)...${NC}"
PID_HSS=$(start hss hss.yaml)
sleep 3

log "${BLUE}[2/5] Starting the edge agent (dea.edge.realm)...${NC}"
PID_DEA=$(start dea dea.yaml)
sleep 3

log "${BLUE}[3/5] Starting the well behaved partner (cli.good.realm)...${NC}"
PID_GOOD=$(start cli_good cli_good.yaml)
sleep 1

log "${BLUE}[4/5] Starting the hostile partner (cli.evil.realm)...${NC}"
PID_EVIL=$(start cli_evil cli_evil.yaml)
sleep 1

log "${BLUE}[5/5] Starting the flooding partner (cli.flood.realm)...${NC}"
PID_FLOOD=$(start cli_flood cli_flood.yaml)
sleep 1

log ""
log "${GREEN}All nodes started:${NC}"
log "  hss.core.realm   PID $PID_HSS"
log "  dea.edge.realm   PID $PID_DEA"
log "  cli.good.realm   PID $PID_GOOD"
log "  cli.evil.realm   PID $PID_EVIL"
log "  cli.flood.realm  PID $PID_FLOOD"
log ""

export PID_HSS PID_DEA PID_GOOD PID_EVIL PID_FLOOD

if [ "$MANUAL_MODE" = true ]; then
    log "${YELLOW}Manual mode: container stays up. Logs are in /tmp/*.log.${NC}"
    log "The admin API is on http://127.0.0.1:9001 and metrics on http://127.0.0.1:9101/metrics."
    tail -f /tmp/*.log
else
    log "${BLUE}Running automated test...${NC}"
    /test.sh
fi
