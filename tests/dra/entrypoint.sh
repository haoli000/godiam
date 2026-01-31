#!/bin/bash
# Entrypoint for godiam DRA test
# Starts 5 godiam instances and runs automated test

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}==============================================${NC}"
echo -e "${BLUE}  godiam DRA Test${NC}"
echo -e "${BLUE}  All 5 nodes running godiam${NC}"
echo -e "${BLUE}==============================================${NC}"
echo ""

# Start DRA 1 (left side)
echo -e "${BLUE}[1/5] Starting godiam dra1 (left side)...${NC}"
/usr/local/bin/diameterd --config /conf/dra1.yaml > /tmp/dra1.log 2>&1 &
export PID_DRA1=$!
sleep 1

# Start peer11 (left side)
echo -e "${BLUE}[2/5] Starting godiam peer11 (left side)...${NC}"
/usr/local/bin/diameterd --config /conf/peer11.yaml > /tmp/peer11.log 2>&1 &
export PID_PEER11=$!
sleep 1

# Start peer12 (left side)
echo -e "${BLUE}[3/5] Starting godiam peer12 (left side)...${NC}"
/usr/local/bin/diameterd --config /conf/peer12.yaml > /tmp/peer12.log 2>&1 &
export PID_PEER12=$!
sleep 1

# Start DRA 2 (right side)
echo -e "${BLUE}[4/5] Starting godiam dra2 (right side)...${NC}"
/usr/local/bin/diameterd --config /conf/dra2.yaml > /tmp/dra2.log 2>&1 &
export PID_DRA2=$!
sleep 1

# Start peer2 (right side) - this sends test messages via test_app
echo -e "${BLUE}[5/5] Starting godiam peer2 (right side)...${NC}"
/usr/local/bin/diameterd --config /conf/peer2.yaml > /tmp/peer2.log 2>&1 &
export PID_PEER2=$!

echo ""
echo -e "${GREEN}All services started:${NC}"
echo -e "  godiam dra1:   PID $PID_DRA1"
echo -e "  godiam peer11: PID $PID_PEER11"
echo -e "  godiam peer12: PID $PID_PEER12"
echo -e "  godiam dra2:   PID $PID_DRA2"
echo -e "  godiam peer2:  PID $PID_PEER2"
echo ""

# Export PIDs for test.sh
export PID_DRA1 PID_PEER11 PID_PEER12 PID_DRA2 PID_PEER2

# Run test mode or manual mode
if [ "$1" = "manual" ]; then
    echo "Manual mode: streaming logs..."
    echo "Press Ctrl+C to exit"
    echo ""
    
    # Tail all logs with prefixes
    tail -f /tmp/dra1.log | sed 's/^/[dra1] /' &
    tail -f /tmp/peer11.log | sed 's/^/[peer11] /' &
    tail -f /tmp/peer12.log | sed 's/^/[peer12] /' &
    tail -f /tmp/dra2.log | sed 's/^/[dra2] /' &
    tail -f /tmp/peer2.log | sed 's/^/[peer2] /' &
    
    wait
else
    echo "Running automated test..."
    echo ""
    
    # Run the test script
    /test.sh
    TEST_RESULT=$?
    
    # Cleanup
    echo ""
    echo "Cleaning up processes..."
    kill $PID_DRA1 $PID_PEER11 $PID_PEER12 $PID_DRA2 $PID_PEER2 2>/dev/null || true
    
    exit $TEST_RESULT
fi
