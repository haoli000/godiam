#!/bin/bash
# Entrypoint for godiam DRA performance test
# Starts server, DRA, and runs diameterbench

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Default benchmark parameters (can be overridden via environment)
BENCH_CONNECTIONS=${BENCH_CONNECTIONS:-10}
BENCH_DURATION=${BENCH_DURATION:-10s}
BENCH_RATE=${BENCH_RATE:-1000}

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam DRA Performance Test${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""

# Start backend server (handles CCRs)
echo -e "${BLUE}[1/3] Starting backend server...${NC}"
/usr/local/bin/diameterd --config /conf/server.yaml > /tmp/server.log 2>&1 &
PID_SERVER=$!
sleep 1

if ! kill -0 $PID_SERVER 2>/dev/null; then
    echo -e "${YELLOW}Server failed to start. Log:${NC}"
    cat /tmp/server.log
    exit 1
fi
echo -e "${GREEN}  Server started (PID $PID_SERVER)${NC}"

# Start DRA (relays messages)
echo -e "${BLUE}[2/3] Starting DRA...${NC}"
/usr/local/bin/diameterd --config /conf/dra.yaml > /tmp/dra.log 2>&1 &
PID_DRA=$!
sleep 2

if ! kill -0 $PID_DRA 2>/dev/null; then
    echo -e "${YELLOW}DRA failed to start. Log:${NC}"
    cat /tmp/dra.log
    kill $PID_SERVER 2>/dev/null || true
    exit 1
fi
echo -e "${GREEN}  DRA started (PID $PID_DRA)${NC}"

# Wait for DRA to connect to server
echo -e "${BLUE}  Waiting for DRA to connect to server...${NC}"
sleep 3

echo ""
echo -e "${BLUE}[3/3] Running diameterbench...${NC}"
echo -e "  Connections: $BENCH_CONNECTIONS"
echo -e "  Duration: $BENCH_DURATION"
echo -e "  Target rate: $BENCH_RATE req/s"
echo ""

# Run benchmark
# If BYPASS_DRA is set, connect directly to server; otherwise go through DRA
if [ "${BYPASS_DRA:-false}" = "true" ]; then
    BENCH_PORT=3868
    BENCH_ID="server.backend.realm"
    echo -e "  Mode: Direct to server (bypassing DRA)"
else
    BENCH_PORT=3869
    BENCH_ID="dra.test.realm"
    echo -e "  Mode: Through DRA"
fi

/usr/local/bin/diameterbench \
    --host 127.0.0.1 \
    --port "$BENCH_PORT" \
    --id "$BENCH_ID" \
    --realm "backend.realm" \
    --conn "$BENCH_CONNECTIONS" \
    --duration "$BENCH_DURATION" \
    --rate "$BENCH_RATE"

BENCH_RESULT=$?

echo ""
echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  Prometheus Metrics${NC}"
echo -e "${BLUE}==============================================${NC}"

# Print DRA metrics (filter to diameter_* lines, skip HELP/TYPE comments)
echo ""
echo -e "${BLUE}--- DRA (dra.test.realm) ---${NC}"
curl -s http://127.0.0.1:9090/metrics 2>/dev/null | grep '^diameter_' || echo "  (metrics unavailable)"

echo ""
echo -e "${BLUE}--- Backend Server (server.backend.realm) ---${NC}"
curl -s http://127.0.0.1:9091/metrics 2>/dev/null | grep '^diameter_' || echo "  (metrics unavailable)"

echo ""
echo -e "${BLUE}==============================================${NC}"

# Cleanup
echo "Cleaning up..."
kill $PID_DRA $PID_SERVER 2>/dev/null || true

if [ $BENCH_RESULT -eq 0 ]; then
    echo -e "${GREEN}Performance test completed successfully${NC}"
else
    echo -e "${YELLOW}Performance test exited with code $BENCH_RESULT${NC}"
fi

exit $BENCH_RESULT
