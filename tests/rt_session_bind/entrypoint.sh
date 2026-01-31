#!/bin/bash
# Entrypoint for godiam rt_session_bind test
# Starts 4 godiam instances: DRA gateway, 2 backend servers, and client
# Validates that session binding routes all requests in a session to the same backend

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam rt_session_bind Test${NC}"
echo -e "${BLUE}  Tests session-affinity routing${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""

# Start DRA gateway first (it must be listening before peers connect)
echo -e "${BLUE}[1/4] Starting DRA gateway (with rt_session_bind)...${NC}"
/usr/local/bin/diameterd --config /conf/gw.yaml > /tmp/gw.log 2>&1 &
export PID_GW=$!
sleep 2

# Start backend1
echo -e "${BLUE}[2/4] Starting backend1 (server1)...${NC}"
/usr/local/bin/diameterd --config /conf/server1.yaml > /tmp/server1.log 2>&1 &
export PID_SERVER1=$!
sleep 1

# Start backend2
echo -e "${BLUE}[3/4] Starting backend2 (server2)...${NC}"
/usr/local/bin/diameterd --config /conf/server2.yaml > /tmp/server2.log 2>&1 &
export PID_SERVER2=$!
sleep 1

# Start client (sends multiple requests with same session)
echo -e "${BLUE}[4/4] Starting client (client, sends test messages)...${NC}"
/usr/local/bin/diameterd --config /conf/client.yaml > /tmp/client.log 2>&1 &
export PID_CLIENT=$!

echo ""
echo -e "${GREEN}All services started:${NC}"
echo -e "  DRA gateway:            PID $PID_GW"
echo -e "  backend1 (server1): PID $PID_SERVER1"
echo -e "  backend2 (server2): PID $PID_SERVER2"
echo -e "  client (client):     PID $PID_CLIENT"
echo ""

# Export PIDs for test.sh
export PID_GW PID_SERVER1 PID_SERVER2 PID_CLIENT

# Run test mode or manual mode
if [ "$1" = "manual" ]; then
    echo "Manual mode: streaming logs..."
    echo "Press Ctrl+C to exit"
    echo ""

    tail -f /tmp/gw.log | sed 's/^/[gw] /' &
    tail -f /tmp/server1.log | sed 's/^/[backend1] /' &
    tail -f /tmp/server2.log | sed 's/^/[backend2] /' &
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
