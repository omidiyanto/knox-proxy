#!/bin/sh
# ══════════════════════════════════════════════════════════════════════════════
# Knox Nginx Entrypoint
# ══════════════════════════════════════════════════════════════════════════════
# Processes nginx.conf template and injects runtime configuration
# based on environment variables before starting nginx.
#
# Environment Variables:
#   KNOX_JIT_ENABLED        - "true" to show JIT Access menu in UI (default: true)
#   KNOX_WATERMARK_ENABLED  - "true" to enable watermark overlay (default: false)
#   KNOX_JIT_EDIT_ENABLED   - "true" to enable JIT Edit checkbox (default: false)
# ══════════════════════════════════════════════════════════════════════════════

set -e

TEMPLATE="/etc/nginx/nginx.conf.template"
OUTPUT="/etc/nginx/nginx.conf"

# Copy template to working config
cp "$TEMPLATE" "$OUTPUT"

# ── JIT UI Toggle ───────────────────────────────────────────────────────────
if [ "${KNOX_JIT_ENABLED}" = "true" ]; then
  echo "[knox-entrypoint] JIT UI ENABLED"
  sed -i 's|__KNOX_JIT_PLACEHOLDER__|<script>window.__KNOX_JIT__=true;</script>|g' "$OUTPUT"
else
  echo "[knox-entrypoint] JIT UI DISABLED"
  sed -i 's|__KNOX_JIT_PLACEHOLDER__||g' "$OUTPUT"
fi

# ── JIT Edit Toggle ─────────────────────────────────────────────────────────
if [ "${KNOX_JIT_EDIT_ENABLED}" = "true" ]; then
  echo "[knox-entrypoint] JIT Edit checkbox ENABLED"
  sed -i 's|__KNOX_JIT_EDIT_PLACEHOLDER__|<script>window.__KNOX_JIT_EDIT__=true;</script>|g' "$OUTPUT"
else
  echo "[knox-entrypoint] JIT Edit checkbox DISABLED (default: run only)"
  sed -i 's|__KNOX_JIT_EDIT_PLACEHOLDER__||g' "$OUTPUT"
fi

# ── Watermark Toggle ────────────────────────────────────────────────────────
if [ "${KNOX_WATERMARK_ENABLED}" = "true" ]; then
  echo "[knox-entrypoint] Watermark ENABLED"
  sed -i 's|__KNOX_WATERMARK_PLACEHOLDER__|<script>window.__KNOX_WATERMARK__=true;</script>|g' "$OUTPUT"
else
  echo "[knox-entrypoint] Watermark DISABLED"
  sed -i 's|__KNOX_WATERMARK_PLACEHOLDER__||g' "$OUTPUT"
fi

echo "[knox-entrypoint] Starting nginx..."
exec nginx -g "daemon off;"

