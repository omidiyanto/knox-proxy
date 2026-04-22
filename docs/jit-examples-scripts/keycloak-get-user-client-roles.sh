#!/bin/bash

export KEYCLOAK_URL="http://localhost:8181"
export ADMIN_USER="admin"
export ADMIN_PASS="admin"
export REALM_TARGET="master"

# Anda bebas mengisi ini dengan USERNAME (o.midiyanto) ATAU EMAIL (o.midiyanto@satnusa.com)
export TARGET_USER="[EMAIL_ADDRESS]" 

echo "[1/3] Mendapatkan access token..."
# 1) Dapatkan Token
export TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/$REALM_TARGET/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=$ADMIN_USER" \
  -d "password=$ADMIN_PASS" \
  -d "grant_type=password" \
  -d "client_id=admin-cli" | jq -r '.access_token')

# Cek apakah token berhasil didapatkan
if [ "$TOKEN" == "null" ] || [ -z "$TOKEN" ]; then
    echo "❌ Gagal mendapatkan token. Silakan periksa kredensial Anda."
    exit 1
fi

echo "✅ Token berhasil didapatkan."
echo "[2/3] Mencari User ID untuk: $TARGET_USER..."

# 2) Deteksi Otomatis: Input berupa Email atau Username?
if [[ "$TARGET_USER" == *"@"* ]]; then
    export QUERY_PARAM="email=$TARGET_USER"
    echo "    -> Mendeteksi input sebagai Email."
else
    export QUERY_PARAM="username=$TARGET_USER"
    echo "    -> Mendeteksi input sebagai Username."
fi

# Request ke Keycloak berdasarkan deteksi di atas
export USER_ID=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/users?$QUERY_PARAM&exact=true" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id')

# Cek apakah User ID ditemukan
if [ "$USER_ID" == "null" ] || [ -z "$USER_ID" ]; then
    echo "❌ Error: User '$TARGET_USER' tidak ditemukan."
    exit 1
fi

echo "✅ User ID ditemukan: $USER_ID"
echo "[3/3] Menarik data role mapping lengkap..."
echo ""

# 3) Ambil Full Role Mappings (Realm + Client)
ROLE_MAPPINGS=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/users/$USER_ID/role-mappings" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN")

# --- PRINT REALM ROLES ---
echo "========================================="
echo "🌍 REALM ROLES:"
echo "========================================="
# Gunakan jq untuk mengekstrak array realmMappings
echo "$ROLE_MAPPINGS" | jq -r '.realmMappings[]?.name | "  - \(.)"'

echo ""

# --- PRINT CLIENT ROLES ---
echo "========================================="
echo "💻 CLIENT ROLES:"
echo "========================================="
# Gunakan jq untuk mengelompokkan berdasarkan nama client, lalu list rolenya
echo "$ROLE_MAPPINGS" | jq -r '.clientMappings | to_entries[]? | "▶ Client: \(.key)", (.value.mappings[]? | "    - \(.name)")'
echo "========================================="