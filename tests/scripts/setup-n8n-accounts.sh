#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
# Setup n8n test accounts and sample workflows
# ══════════════════════════════════════════════════════════════════════════════
# Runs AFTER n8n is healthy. Handles first-time setup and creates test data.
# ══════════════════════════════════════════════════════════════════════════════
set -e

N8N_URL="${N8N_URL:-http://localhost:5678}"
OWNER_EMAIL="owner@knox-ci.local"
OWNER_PASSWORD="ownerPass123"
OWNER_FIRST="CI"
OWNER_LAST="Owner"
TEAM_EMAIL="${TEAM_EMAIL:-test@local}"
TEAM_PASSWORD="${TEAM_PASSWORD:-testPass123}"
TEAM_FIRST="Test"
TEAM_LAST="Team"
ENV_FILE="$(dirname "$0")/../.test-env"

echo "═══════════════════════════════════════════════════════════"
echo "  Knox CI — Setting up n8n accounts..."
echo "═══════════════════════════════════════════════════════════"

# ── Step 1: Check if n8n needs first-time setup ────────────────────────────
echo "→ Checking n8n setup status..."
SETUP_STATUS=$(curl -sf "${N8N_URL}/rest/settings" 2>/dev/null | grep -o '"userManagement"' || true)

# Try to run the owner setup (idempotent — fails silently if already done)
echo "→ Running owner setup..."
SETUP_RESPONSE=$(curl -sf -X POST "${N8N_URL}/rest/owner/setup" \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"${OWNER_EMAIL}\",
    \"password\": \"${OWNER_PASSWORD}\",
    \"firstName\": \"${OWNER_FIRST}\",
    \"lastName\": \"${OWNER_LAST}\"
  }" 2>/dev/null || echo '{"already":"done"}')

echo "  Owner setup: $SETUP_RESPONSE"

# ── Step 2: Login as owner to get auth cookie ──────────────────────────────
echo "→ Logging in as owner..."
COOKIE_JAR=$(mktemp)

# Remove -f so we can capture the actual HTTP error body if it fails
LOGIN_RESPONSE=$(curl -s -c "$COOKIE_JAR" -w "\nHTTP_STATUS:%{http_code}" -X POST "${N8N_URL}/rest/login" \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"${OWNER_EMAIL}\",
    \"password\": \"${OWNER_PASSWORD}\"
  }" || true)

HTTP_STATUS=$(echo "$LOGIN_RESPONSE" | grep -o "HTTP_STATUS:[0-9]*" | cut -d':' -f2)
LOGIN_BODY=$(echo "$LOGIN_RESPONSE" | sed 's/HTTP_STATUS:[0-9]*//g')

if [ "$HTTP_STATUS" != "200" ]; then
  echo "✗ Failed to login as owner (HTTP $HTTP_STATUS)"
  echo "  Response: $LOGIN_BODY"
  rm -f "$COOKIE_JAR"
  exit 1
fi
echo "  ✓ Logged in as owner"

# ── Step 3: Create team user ───────────────────────────────────────────────
echo "→ Creating team user: ${TEAM_EMAIL}..."
INVITE_RESPONSE=$(curl -s -b "$COOKIE_JAR" -w "\nHTTP_STATUS:%{http_code}" -X POST "${N8N_URL}/rest/invitations" \
  -H "Content-Type: application/json" \
  -d "[{
    \"email\": \"${TEAM_EMAIL}\",
    \"role\": \"global:member\"
  }]" || true)

HTTP_STATUS=$(echo "$INVITE_RESPONSE" | grep -o "HTTP_STATUS:[0-9]*" | cut -d':' -f2)
INVITE_BODY=$(echo "$INVITE_RESPONSE" | sed 's/HTTP_STATUS:[0-9]*//g')

if [ "$HTTP_STATUS" != "200" ]; then
    echo "  Team user invite failed (HTTP $HTTP_STATUS): $INVITE_BODY"
else
    echo "  Team user invite: $INVITE_BODY"
fi

# Extract invitation ID and accept it
INVITE_ID=$(echo "$INVITE_BODY" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -n "$INVITE_ID" ]; then
  echo "→ Accepting invitation: ${INVITE_ID}..."
  ACCEPT_RESPONSE=$(curl -s -X POST "${N8N_URL}/rest/invitations/${INVITE_ID}/accept" \
    -w "\nHTTP_STATUS:%{http_code}" \
    -H "Content-Type: application/json" \
    -d "{
      \"firstName\": \"${TEAM_FIRST}\",
      \"lastName\": \"${TEAM_LAST}\",
      \"password\": \"${TEAM_PASSWORD}\"
    }" || true)
  
  if echo "$ACCEPT_RESPONSE" | grep -q "HTTP_STATUS:200"; then
    echo "  ✓ Team user created"
  else
    echo "  ✗ Failed to accept test team invite: $ACCEPT_RESPONSE"
  fi
else
  echo "  ℹ Team user may already exist"
fi

# ── Step 4: Create a sample workflow ───────────────────────────────────────
echo "→ Creating sample workflow..."
WF_RESPONSE=$(curl -s -b "$COOKIE_JAR" -w "\nHTTP_STATUS:%{http_code}" -X POST "${N8N_URL}/rest/workflows" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Knox CI Test Workflow",
    "nodes": [
      {
        "parameters": {},
        "id": "start-node",
        "name": "Start",
        "type": "n8n-nodes-base.manualTrigger",
        "typeVersion": 1,
        "position": [250, 300]
      },
      {
        "parameters": {
          "values": {
            "string": [{"name": "message", "value": "Knox CI Test OK"}]
          },
          "options": {}
        },
        "id": "set-node",
        "name": "Set",
        "type": "n8n-nodes-base.set",
        "typeVersion": 3.4,
        "position": [450, 300]
      }
    ],
    "connections": {
      "Start": {"main": [[{"node": "Set", "type": "main", "index": 0}]]}
    },
    "settings": {},
    "staticData": null
  }' || true)

WORKFLOW_ID=$(echo "$WF_RESPONSE" | head -n -1 | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -n "$WORKFLOW_ID" ]; then
  echo "  ✓ Workflow created: ${WORKFLOW_ID}"
else
  # Try to get existing workflows
  WORKFLOW_ID=$(curl -sf -b "$COOKIE_JAR" "${N8N_URL}/rest/workflows" 2>/dev/null \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  echo "  ℹ Using existing workflow: ${WORKFLOW_ID}"
fi

# ── Step 5: Export env file for tests ──────────────────────────────────────
echo "→ Writing test environment to ${ENV_FILE}..."
cat > "$ENV_FILE" <<EOF
# Auto-generated by setup-n8n-accounts.sh
KNOX_TEST_BASE_URL=http://localhost:8443
KEYCLOAK_URL=http://localhost:8080
N8N_URL=http://localhost:5678
VAULT_ADDR=http://localhost:8200
TEST_WORKFLOW_ID=${WORKFLOW_ID}
TEST_USER=testuser
TEST_PASS=testpass
RESTRICTED_USER=restricteduser
RESTRICTED_PASS=testpass
ADMIN_USER=adminuser
ADMIN_PASS=testpass
KNOX_API_KEY=test-api-key-for-ci
EOF

echo "  ✓ Test env written"

# Cleanup
rm -f "$COOKIE_JAR"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  ✓ n8n setup complete!"
echo "  Workflow ID: ${WORKFLOW_ID}"
echo "═══════════════════════════════════════════════════════════"
