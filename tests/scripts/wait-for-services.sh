#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
# Wait for all Knox test services to be ready
# ══════════════════════════════════════════════════════════════════════════════
set -e

TIMEOUT=300
INTERVAL=5
ELAPSED=0

echo "═══════════════════════════════════════════════════════════"
echo "  Knox CI — Waiting for services..."
echo "═══════════════════════════════════════════════════════════"

wait_for() {
  local NAME="$1"
  local URL="$2"
  local START=$ELAPSED

  while true; do
    if [ $ELAPSED -ge $TIMEOUT ]; then
      echo "✗ TIMEOUT: $NAME did not become ready within ${TIMEOUT}s"
      exit 1
    fi

    if curl -sf --max-time 5 "$URL" > /dev/null 2>&1; then
      echo "✓ $NAME is ready (${ELAPSED}s)"
      return 0
    fi

    sleep $INTERVAL
    ELAPSED=$((ELAPSED + INTERVAL))
    echo "  ⏳ Waiting for $NAME... (${ELAPSED}s)"
  done
}

# ── Check Keycloak ──────────────────────────────────────────────────────────
wait_for "Keycloak" "http://localhost:8080/health/ready"

# ── Check Keycloak OIDC Discovery ───────────────────────────────────────────
wait_for "Keycloak OIDC" "http://localhost:8080/realms/knox-test/.well-known/openid-configuration"

# ── Check Vault ─────────────────────────────────────────────────────────────
wait_for "Vault" "http://localhost:8200/v1/sys/health"

# ── Check n8n ───────────────────────────────────────────────────────────────
# n8n may return non-200 during initial setup, so we check /healthz
wait_for "n8n" "http://localhost:5678/healthz"

# ── Check Knox ──────────────────────────────────────────────────────────────
wait_for "Knox Proxy" "http://localhost:8443/healthz"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  ✓ All services are ready! (total: ${ELAPSED}s)"
echo "═══════════════════════════════════════════════════════════"
