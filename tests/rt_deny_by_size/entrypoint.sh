#!/bin/bash
# Entrypoint for godiam rt_deny_by_size test

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam rt_deny_by_size Test${NC}"
echo -e "${BLUE}  Tests message size limit enforcement${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""

echo -e "${BLUE}[1/3] Starting godiam gw (with rt_deny_by_size max=100)...${NC}"
/usr/local/bin/diameterd --config /conf/gw.yaml > /tmp/gw.log 2>&1 &
export PID_GW=$!
sleep 1

echo -e "${BLUE}[2/3] Starting godiam server...${NC}"
/usr/local/bin/diameterd --config /conf/server.yaml > /tmp/server.log 2>&1 &
export PID_SERVER=$!
sleep 1

echo -e "${BLUE}[3/3] Starting godiam client (sends test messages)...${NC}"
/usr/local/bin/diameterd --config /conf/client.yaml > /tmp/client.log 2>&1 &
export PID_CLIENT=$!

echo ""
echo -e "${GREEN}All services started:${NC}"
echo -e "  godiam gw:         PID $PID_GW"
echo -e "  godiam server: PID $PID_SERVER"
echo -e "  godiam client:  PID $PID_CLIENT"
echo ""

export PID_GW PID_SERVER PID_CLIENT

if [ "$1" = "manual" ]; then
    echo "Manual mode: streaming logs..."
    echo "Press Ctrl+C to exit"
    echo ""

    tail -f /tmp/gw.log | sed 's/^/[gw] /' &
    tail -f /tmp/server.log | sed 's/^/[server] /' &
    tail -f /tmp/client.log | sed 's/^/[client] /' &

    wait
else
    echo "Running automated test..."
    echo ""

    /test.sh
    TEST_RESULT=$?

    echo ""
    echo "Cleaning up processes..."
    kill $PID_GW $PID_SERVER $PID_CLIENT 2>/dev/null || true

    exit $TEST_RESULT
fi
