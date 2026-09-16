#!/bin/bash
# Verification for the Diameter Edge Agent integration test.
#
# Every assertion is made against the node logs, the admin API and the metrics
# endpoint of the edge agent, so the test exercises the same surfaces an
# operator would use.

set -u

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

WAIT_FOR_TRAFFIC=${WAIT_FOR_TRAFFIC:-15}
ADMIN_URL=${ADMIN_URL:-http://127.0.0.1:9001}
METRICS_URL=${METRICS_URL:-http://127.0.0.1:9101/metrics}

PASS=0
FAIL=0

log() { echo -e "$@"; }

ok() {
    log "${GREEN}✓ $1${NC}"
    PASS=$((PASS + 1))
}

ko() {
    log "${RED}✗ $1${NC}"
    FAIL=$((FAIL + 1))
}

# expect_log NAME DESCRIPTION FILE PATTERN
expect_log() {
    local desc=$1 file=$2 pattern=$3
    if grep -qE "$pattern" "$file" 2>/dev/null; then
        ok "$desc"
    else
        ko "$desc (no match for /$pattern/ in $file)"
    fi
}

# reject_log DESCRIPTION FILE PATTERN — the pattern must NOT appear.
reject_log() {
    local desc=$1 file=$2 pattern=$3
    if grep -qE "$pattern" "$file" 2>/dev/null; then
        ko "$desc (unexpected match for /$pattern/ in $file)"
    else
        ok "$desc"
    fi
}

check_process() {
    local pid=$1 name=$2
    if kill -0 "$pid" 2>/dev/null; then
        ok "$name is running (PID $pid)"
    else
        ko "$name (PID $pid) has died"
    fi
}

log ""
log "${BLUE}=========================================${NC}"
log "${BLUE}  Edge Agent Verification${NC}"
log "${BLUE}=========================================${NC}"
log ""
log "${YELLOW}Waiting ${WAIT_FOR_TRAFFIC}s for peer connections and traffic...${NC}"
sleep "$WAIT_FOR_TRAFFIC"

log ""
log "${BLUE}--- Processes ---${NC}"
check_process "$PID_HSS" "hss.core.realm"
check_process "$PID_DEA" "dea.edge.realm"
check_process "$PID_GOOD" "cli.good.realm"
check_process "$PID_EVIL" "cli.evil.realm"
check_process "$PID_FLOOD" "cli.flood.realm"

log ""
log "${BLUE}--- Zones and admission ---${NC}"
expect_log "internal zone listener started" /tmp/dea.log 'Zone "core" listening'
expect_log "external zone listener started" /tmp/dea.log 'Zone "roaming" listening'
expect_log "internal peer connected" /tmp/dea.log 'hss\.core\.realm'

log ""
log "${BLUE}--- Relaying for a well behaved partner ---${NC}"
expect_log "partner request reached the internal node" /tmp/hss.log 'test_app request:.*from=cli\.good\.realm'
expect_log "partner received a successful answer" /tmp/cli_good.log 'test_app result: 2001'

log ""
log "${BLUE}--- Topology hiding ---${NC}"
expect_log "answer carries a pseudonym Origin-Host" /tmp/cli_good.log 'test_app answer:.*from=th[0-9a-f]+\.edge\.realm'
reject_log "internal identity never reaches the partner" /tmp/cli_good.log 'hss\.core\.realm'
reject_log "internal realm never reaches the partner" /tmp/cli_good.log 'core\.realm/'

log ""
log "${BLUE}--- Ingress screening ---${NC}"
expect_log "disallowed application is screened" /tmp/dea.log 'dea_screening: action=reject rule=application partner=partner-evil'
expect_log "hostile partner is answered 5003" /tmp/cli_evil.log 'test_app result: 5003'
reject_log "screened request never reached the internal node" /tmp/hss.log 'from=cli\.evil\.realm'

log ""
log "${BLUE}--- Rate limiting ---${NC}"
expect_log "burst above the partner limit is throttled" /tmp/dea.log 'dea_ratelimit: action=reject scope=partner partner=partner-flood'
expect_log "throttled partner is answered 3004" /tmp/cli_flood.log 'test_app result: 3004'
expect_log "traffic inside the burst is still relayed" /tmp/cli_flood.log 'test_app result: 2001'

log ""
log "${BLUE}--- Admin API ---${NC}"
EDGE_JSON=$(curl -sf "$ADMIN_URL/api/v1/edge" || true)
if echo "$EDGE_JSON" | grep -q '"configured":true'; then
    ok "admin API reports the edge model as configured"
else
    ko "admin API did not report a configured edge model"
fi
for partner in partner-good partner-evil partner-flood; do
    if echo "$EDGE_JSON" | grep -q "\"$partner\""; then
        ok "admin API lists $partner"
    else
        ko "admin API does not list $partner"
    fi
done
if echo "$EDGE_JSON" | grep -qE 'key|private|password'; then
    ko "admin API leaked key material"
else
    ok "admin API exposes no key material"
fi

log ""
log "${BLUE}--- Metrics ---${NC}"
METRICS=$(curl -sf "$METRICS_URL" || true)
for metric in diameter_edge_zones diameter_edge_partners diameter_edge_partner_peers_up; do
    if echo "$METRICS" | grep -q "^$metric"; then
        ok "metric $metric is exported"
    else
        ko "metric $metric is missing"
    fi
done
if echo "$METRICS" | grep -q 'diameter_peer_up{.*zone='; then
    ok "peer metrics carry the zone label"
else
    ko "peer metrics are missing the zone label"
fi

log ""
log "${BLUE}=========================================${NC}"
log "  Passed: ${GREEN}${PASS}${NC}   Failed: ${RED}${FAIL}${NC}"
log "${BLUE}=========================================${NC}"

if [ "$FAIL" -ne 0 ]; then
    log ""
    log "${YELLOW}Edge agent log tail:${NC}"
    tail -40 /tmp/dea.log
    exit 1
fi

log ""
log "${GREEN}All edge agent assertions passed.${NC}"
exit 0
