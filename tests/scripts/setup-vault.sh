#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
# Setup Vault dev server with AppRole auth and test secrets
# ══════════════════════════════════════════════════════════════════════════════
set -e

export VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
export VAULT_TOKEN="vault-test-root-token"
ENV_FILE="$(dirname "$0")/../.test-env"

echo "═══════════════════════════════════════════════════════════"
echo "  Knox CI — Setting up Vault..."
echo "═══════════════════════════════════════════════════════════"

# ── Step 1: Enable AppRole auth ────────────────────────────────────────────
echo "→ Enabling AppRole auth method..."
curl -sf -X POST "${VAULT_ADDR}/v1/sys/auth/approle" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -d '{"type":"approle"}' 2>/dev/null || echo "  ℹ AppRole may already be enabled"

# ── Step 2: Create policy for Knox ─────────────────────────────────────────
echo "→ Creating Knox read policy..."
curl -sf -X PUT "${VAULT_ADDR}/v1/sys/policies/acl/knox-read" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -d '{
    "policy": "path \"secret/data/knox/*\" { capabilities = [\"read\", \"list\"] }"
  }'

# ── Step 3: Create AppRole role ────────────────────────────────────────────
echo "→ Creating AppRole role: knox-proxy..."
curl -sf -X POST "${VAULT_ADDR}/v1/auth/approle/role/knox-proxy" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -d '{
    "token_policies": ["knox-read"],
    "token_ttl": "1h",
    "token_max_ttl": "4h",
    "secret_id_ttl": "0",
    "bind_secret_id": true
  }'

# ── Step 4: Get role-id ────────────────────────────────────────────────────
echo "→ Retrieving role-id..."
ROLE_ID=$(curl -sf "${VAULT_ADDR}/v1/auth/approle/role/knox-proxy/role-id" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" | grep -o '"role_id":"[^"]*"' | cut -d'"' -f4)
echo "  ✓ Role ID: ${ROLE_ID}"

# ── Step 5: Generate secret-id ─────────────────────────────────────────────
echo "→ Generating secret-id..."
SECRET_ID=$(curl -sf -X POST "${VAULT_ADDR}/v1/auth/approle/role/knox-proxy/secret-id" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" | grep -o '"secret_id":"[^"]*"' | cut -d'"' -f4)
echo "  ✓ Secret ID: ${SECRET_ID:0:8}..."

# ── Step 6: Enable KV v2 secrets engine (if not already) ──────────────────
echo "→ Enabling KV v2 secrets engine..."
curl -sf -X POST "${VAULT_ADDR}/v1/sys/mounts/secret" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -d '{"type":"kv","options":{"version":"2"}}' 2>/dev/null || echo "  ℹ KV v2 may already be enabled"

# ── Step 7: Write team credentials to Vault ────────────────────────────────
echo "→ Writing team credentials to Vault..."
curl -sf -X POST "${VAULT_ADDR}/v1/secret/data/knox/n8n_teams" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "testteam": {
        "email": "test@local",
        "pass": "testPass123"
      }
    }
  }'
echo "  ✓ Team credentials written"

# ── Step 8: Write Knox config secrets ──────────────────────────────────────
echo "→ Writing Knox config to Vault..."
curl -sf -X POST "${VAULT_ADDR}/v1/secret/data/knox/config" \
  -H "X-Vault-Token: ${VAULT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "knox_api_key": "vault-api-key-for-ci",
      "oidc_client_secret": "knox-test-secret"
    }
  }'
echo "  ✓ Knox config written"

# ── Step 9: Append Vault config to test env file ──────────────────────────
echo "→ Appending Vault config to .test-env..."
if [ -f "$ENV_FILE" ]; then
  cat >> "$ENV_FILE" <<EOF
VAULT_ROLE_ID=${ROLE_ID}
VAULT_SECRET_ID=${SECRET_ID}
VAULT_TEAMS_PATH=secret/data/knox/n8n_teams
VAULT_CONFIG_PATH=secret/data/knox/config
EOF
else
  cat > "$ENV_FILE" <<EOF
VAULT_ADDR=${VAULT_ADDR}
VAULT_ROLE_ID=${ROLE_ID}
VAULT_SECRET_ID=${SECRET_ID}
VAULT_TEAMS_PATH=secret/data/knox/n8n_teams
VAULT_CONFIG_PATH=secret/data/knox/config
EOF
fi
echo "  ✓ Vault env appended"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  ✓ Vault setup complete!"
echo "  Role ID:   ${ROLE_ID}"
echo "  Secret ID: ${SECRET_ID:0:8}..."
echo "═══════════════════════════════════════════════════════════"
