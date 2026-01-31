#!/bin/bash
# Entrypoint for godiam rt_ereg test

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam rt_ereg Test${NC}"
echo -e "${BLUE}  Tests regex-based routing${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""

echo -e "${BLUE}[1/4] Starting godiam gw (with rt_ereg)...${NC}"
/usr/local/bin/diameterd --config /conf/gw.yaml > /tmp/gw.log 2>&1 &
export PID_GW=$!
sleep 1

echo -e "${BLUE}[2/4] Starting godiam server1 (boosted)...${NC}"
/usr/local/bin/diameterd --config /conf/server1.yaml > /tmp/server1.log 2>&1 &
export PID_SERVER1=$!
sleep 1

echo -e "${BLUE}[3/4] Starting godiam server2...${NC}"
/usr/local/bin/diameterd --config /conf/server2.yaml > /tmp/server2.log 2>&1 &
export PID_SERVER2=$!
sleep 1

echo -e "${BLUE}[4/4] Starting godiam client (sends 5 test messages)...${NC}"
/usr/local/bin/diameterd --config /conf/client.yaml > /tmp/client.log 2>&1 &
export PID_CLIENT=$!

echo ""
echo -e "${GREEN}All services started:${NC}"
echo -e "  godiam gw:          PID $PID_GW"
echo -e "  godiam server1: PID $PID_SERVER1"
echo -e "  godiam server2: PID $PID_SERVER2"
echo -e "  godiam client:   PID $PID_CLIENT"
echo ""

export PID_GW PID_SERVER1 PID_SERVER2 PID_CLIENT

if [ "$1" = "manual" ]; then
    echo "Manual mode: streaming logs..."
    echo "Press Ctrl+C to exit"
    echo ""

    tail -f /tmp/gw.log | sed 's/^/[gw] /' &
    tail -f /tmp/server1.log | sed 's/^/[server1] /' &
    tail -f /tmp/server2.log | sed 's/^/[server2] /' &
    tail -f /tmp/client.log | sed 's/^/[client] /' &

    wait
else
    echo "Running automated test..."
    echo ""

    /test.sh
    TEST_RESULT=$?

    echo ""
    echo "Cleaning up processes..."
    kill $PID_GW $PID_SERVER1 $PID_SERVER2 $PID_CLIENT 2>/dev/null || true

    exit $TEST_RESULT
fi
