#!/bin/bash


export KEYCLOAK_URL="http://localhost:8181"
export ADMIN_USER="admin"
export ADMIN_PASS="admin"

export AUTH_REALM="master"
export REALM_TARGET="master"
# ----------------------------------------------------------------------------

export TARGET_USER="[EMAIL_ADDRESS]"

echo "[1/3] Mendapatkan access token administrator dari realm '$AUTH_REALM'..."
# 1) Dapatkan Token (menggunakan AUTH_REALM)
export TOKEN=$(curl -s -X POST "$KEYCLOAK_URL/realms/$AUTH_REALM/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=$ADMIN_USER" \
  -d "password=$ADMIN_PASS" \
  -d "grant_type=password" \
  -d "client_id=admin-cli" | jq -r '.access_token')

# Cek apakah token berhasil didapatkan
if [ "$TOKEN" == "null" ] || [ -z "$TOKEN" ]; then
    echo "❌ Gagal mendapatkan token. Silakan periksa kredensial administrator Anda."
    exit 1
fi
echo "✅ Token administrator berhasil didapatkan."

echo ""
echo "[2/3] Mencari User ID untuk: $TARGET_USER di realm '$REALM_TARGET'..."

# Deteksi Otomatis: Input berupa Email atau Username
if [[ "$TARGET_USER" == *"@"* ]]; then
    export QUERY_PARAM="email=$TARGET_USER"
else
    export QUERY_PARAM="username=$TARGET_USER"
fi

# Request ke Keycloak untuk mendapatkan UUID (menggunakan REALM_TARGET)
export USER_ID=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/users?$QUERY_PARAM&exact=true" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id')

# Cek apakah User ID ditemukan
if [ "$USER_ID" == "null" ] || [ -z "$USER_ID" ]; then
    echo "❌ Error: User '$TARGET_USER' tidak ditemukan di Keycloak."
    exit 1
fi
echo "✅ User ID ditemukan: $USER_ID"

echo ""
echo "[3/3] Mengeksekusi Backchannel Logout..."
echo "========================================="
echo "🛑 Mematikan semua sesi untuk: $TARGET_USER"

# 3) Eksekusi Admin API untuk logout paksa (menggunakan REALM_TARGET)
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/users/$USER_ID/logout" \
  -H "Authorization: Bearer $TOKEN")

# Validasi hasil (Keycloak mengembalikan status 204 No Content jika sukses)
if [ "$HTTP_STATUS" -eq 204 ]; then
    echo "✅ BERHASIL! Sesi user telah dimatikan dari server Keycloak."
    echo "   (Sinyal Backchannel Logout otomatis dikirim ke client terkait)"
else
    echo "❌ GAGAL melakukan logout. HTTP Status: $HTTP_STATUS"
fi
echo "========================================="
