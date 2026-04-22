#!/bin/bash

export KEYCLOAK_URL="http://localhost:8181"
export ADMIN_USER="admin"
export ADMIN_PASS="admin"

export AUTH_REALM="master"
export REALM_TARGET="master"

# --- TARGET CLIENT & ROLE BARU ---
export CLIENT_NAME="n8n-proxy"
export NEW_ROLE_NAME="run:abcdef"
# ---------------------------------

echo "[1/3] Mendapatkan access token administrator..."
export TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/$AUTH_REALM/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=$ADMIN_USER" \
  -d "password=$ADMIN_PASS" \
  -d "grant_type=password" \
  -d "client_id=admin-cli" | jq -r '.access_token')

if [ "$TOKEN" == "null" ] || [ -z "$TOKEN" ]; then
    echo "❌ Gagal mendapatkan token."
    exit 1
fi
echo "✅ Token administrator berhasil didapatkan."

echo ""
echo "[2/3] Mencari UUID untuk Client: $CLIENT_NAME..."

# 2) Dapatkan Client UUID berdasarkan Client ID (string)
export CLIENT_UUID=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/clients?clientId=$CLIENT_NAME" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id')

if [ "$CLIENT_UUID" == "null" ] || [ -z "$CLIENT_UUID" ]; then
    echo "❌ Error: Client '$CLIENT_NAME' tidak ditemukan di realm '$REALM_TARGET'."
    exit 1
fi
echo "✅ Client UUID ditemukan: $CLIENT_UUID"

echo ""
echo "[3/3] Membuat Role Baru: '$NEW_ROLE_NAME' ..."

# 3) Eksekusi POST untuk membuat Client Role
# Keycloak mengharapkan JSON payload berisi setidaknya "name"
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/clients/$CLIENT_UUID/roles" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d "{\"name\": \"$NEW_ROLE_NAME\", \"description\": \"Dibuat otomatis via API\"}")

echo "========================================="
# Validasi Hasil
if [ "$HTTP_STATUS" -eq 201 ]; then
    echo "✅ BERHASIL! Role '$NEW_ROLE_NAME' sukses dibuat di client '$CLIENT_NAME'."
elif [ "$HTTP_STATUS" -eq 409 ]; then
    echo "⚠️  INFO: Role '$NEW_ROLE_NAME' sudah ada sebelumnya (Conflict/Duplikat)."
elif [ "$HTTP_STATUS" -eq 403 ]; then
    echo "❌ GAGAL (403 Forbidden): User admin Anda kekurangan role 'manage-clients'."
else
    echo "❌ GAGAL: Terjadi kesalahan. HTTP Status: $HTTP_STATUS"
fi
echo "========================================="
