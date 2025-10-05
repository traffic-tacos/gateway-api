#!/bin/bash

# Test script to verify position update fix (v1.3.1)
# This script tests that position decreases when other users enter

BASE_URL="https://api.traffictacos.store/api/v1"
EVENT_ID="test-event-$(date +%s)"

echo "=================================="
echo "Position Update Test (v1.3.1)"
echo "=================================="
echo ""

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Step 1: Create 5 users and join queue
echo "📝 Step 1: Creating 5 users and joining queue..."
TOKENS=()

for i in {1..5}; do
  echo -n "  User $i joining... "
  
  RESPONSE=$(curl -s -X POST "$BASE_URL/queue/join" \
    -H "Content-Type: application/json" \
    -d "{\"event_id\": \"$EVENT_ID\", \"user_id\": \"user$i\"}")
  
  TOKEN=$(echo $RESPONSE | jq -r '.waiting_token')
  POSITION=$(echo $RESPONSE | jq -r '.position_hint')
  
  if [ "$TOKEN" != "null" ]; then
    TOKENS+=("$TOKEN")
    echo -e "${GREEN}✓${NC} Token: ${TOKEN:0:8}... Position: $POSITION"
  else
    echo -e "${RED}✗${NC} Failed!"
    echo "Response: $RESPONSE"
    exit 1
  fi
  
  sleep 0.5
done

echo ""
echo "✅ All 5 users joined successfully!"
echo ""

# Step 2: Check User 5's initial position
echo "📊 Step 2: Checking User 5's initial position..."
USER5_TOKEN="${TOKENS[4]}"

RESPONSE=$(curl -s "$BASE_URL/queue/status?token=$USER5_TOKEN")
INITIAL_POSITION=$(echo $RESPONSE | jq -r '.position')

echo "  User 5 initial position: $INITIAL_POSITION"
echo ""

# Step 3: User 1 enters (should reduce positions for others)
echo "🚪 Step 3: User 1 entering..."
sleep 5 # Wait minimum 5 seconds (eligibility requirement)

ENTER_RESPONSE=$(curl -s -X POST "$BASE_URL/queue/enter" \
  -H "Content-Type: application/json" \
  -d "{\"waiting_token\": \"${TOKENS[0]}\"}")

ADMISSION=$(echo $ENTER_RESPONSE | jq -r '.admission')

if [ "$ADMISSION" == "granted" ]; then
  echo -e "  ${GREEN}✓${NC} User 1 entered successfully"
else
  echo -e "  ${YELLOW}⚠${NC} User 1 not ready yet (wait time or rate limit)"
  echo "  Response: $ENTER_RESPONSE"
fi

sleep 2
echo ""

# Step 4: Check User 5's position again
echo "📊 Step 4: Checking User 5's position after User 1 entered..."
RESPONSE=$(curl -s "$BASE_URL/queue/status?token=$USER5_TOKEN")
AFTER_POSITION=$(echo $RESPONSE | jq -r '.position')

echo "  User 5 position after User 1 entered: $AFTER_POSITION"
echo ""

# Step 5: User 2 enters
echo "🚪 Step 5: User 2 entering..."
sleep 3

ENTER_RESPONSE=$(curl -s -X POST "$BASE_URL/queue/enter" \
  -H "Content-Type: application/json" \
  -d "{\"waiting_token\": \"${TOKENS[1]}\"}")

ADMISSION=$(echo $ENTER_RESPONSE | jq -r '.admission')

if [ "$ADMISSION" == "granted" ]; then
  echo -e "  ${GREEN}✓${NC} User 2 entered successfully"
else
  echo -e "  ${YELLOW}⚠${NC} User 2 not ready yet"
fi

sleep 2
echo ""

# Step 6: Check User 5's position again
echo "📊 Step 6: Checking User 5's position after User 2 entered..."
RESPONSE=$(curl -s "$BASE_URL/queue/status?token=$USER5_TOKEN")
FINAL_POSITION=$(echo $RESPONSE | jq -r '.position')

echo "  User 5 position after User 2 entered: $FINAL_POSITION"
echo ""

# Step 7: Verify position decreased
echo "=================================="
echo "📈 Results Summary:"
echo "=================================="
echo "User 5 Positions:"
echo "  Initial:      $INITIAL_POSITION"
echo "  After User 1: $AFTER_POSITION"
echo "  After User 2: $FINAL_POSITION"
echo ""

if [ "$FINAL_POSITION" -lt "$INITIAL_POSITION" ]; then
  echo -e "${GREEN}✅ TEST PASSED!${NC}"
  echo "   Position decreased from $INITIAL_POSITION to $FINAL_POSITION"
  echo "   This confirms the bugfix is working correctly! 🎉"
  EXIT_CODE=0
else
  echo -e "${RED}❌ TEST FAILED!${NC}"
  echo "   Position did NOT decrease (still $FINAL_POSITION)"
  echo "   Expected: < $INITIAL_POSITION"
  echo "   This indicates the bug is still present or needs more time to propagate"
  EXIT_CODE=1
fi

echo ""

# Step 8: Cleanup (leave remaining users)
echo "🧹 Cleaning up remaining users..."
for i in {2..4}; do
  curl -s -X DELETE "$BASE_URL/queue/leave?token=${TOKENS[$i]}" > /dev/null
  echo "  User $(($i + 1)) left"
done

echo ""
echo "=================================="
echo "Test completed!"
echo "=================================="

exit $EXIT_CODE
