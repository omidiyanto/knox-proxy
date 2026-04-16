#!/bin/bash

export KEYCLOAK_URL="http://localhost:8181"
export ADMIN_USER="admin"
export ADMIN_PASS="admin"

export AUTH_REALM="master"
export REALM_TARGET="master"

# --- TARGET PENCABUTAN ROLE ---
export TARGET_EMAIL="user@gmail.com"
export CLIENT_NAME="n8n-proxy"
export ROLE_TO_REVOKE="run:abcdef"
# ------------------------------

echo "[1/5] Mendapatkan access token administrator..."
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
echo "[2/5] Mencari User ID berdasarkan email: $TARGET_EMAIL..."
export USER_ID=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/users?email=$TARGET_EMAIL&exact=true" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id')

if [ "$USER_ID" == "null" ] || [ -z "$USER_ID" ]; then
    echo "❌ Error: User dengan email '$TARGET_EMAIL' tidak ditemukan."
    exit 1
fi
echo "✅ User ID ditemukan: $USER_ID"

echo ""
echo "[3/5] Mencari UUID untuk Client: $CLIENT_NAME..."
export CLIENT_UUID=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/clients?clientId=$CLIENT_NAME" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.[0].id')

if [ "$CLIENT_UUID" == "null" ] || [ -z "$CLIENT_UUID" ]; then
    echo "❌ Error: Client '$CLIENT_NAME' tidak ditemukan."
    exit 1
fi
echo "✅ Client UUID ditemukan: $CLIENT_UUID"

echo ""
echo "[4/5] Mengambil detail Role Object dari role '$ROLE_TO_REVOKE'..."
# Keycloak membutuhkan JSON dari role ini untuk memberi tahu role mana yang akan dihapus dari mapping
export ROLE_JSON=$(curl -s -X GET "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/clients/$CLIENT_UUID/roles/$ROLE_TO_REVOKE" \
  -H "Accept: application/json" \
  -H "Authorization: Bearer $TOKEN")

ROLE_ID=$(echo "$ROLE_JSON" | jq -r '.id')
if [ "$ROLE_ID" == "null" ] || [ -z "$ROLE_ID" ]; then
    echo "❌ Error: Role '$ROLE_TO_REVOKE' tidak ditemukan di client '$CLIENT_NAME'."
    exit 1
fi
echo "✅ Role ditemukan. Role ID: $ROLE_ID"

echo ""
echo "[5/5] Mencabut (Revoke) role dari user..."

# Eksekusi DELETE untuk menghapus role mapping. 
# Walaupun DELETE, Keycloak mensyaratkan body JSON (berupa array) yang berisi role yang ingin dicabut.
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$KEYCLOAK_URL/admin/realms/$REALM_TARGET/users/$USER_ID/role-mappings/clients/$CLIENT_UUID" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d "[$ROLE_JSON]")

echo "========================================="
# Validasi Hasil
if [ "$HTTP_STATUS" -eq 204 ]; then
    echo "✅ BERHASIL! Role '$ROLE_TO_REVOKE' telah sukses dicabut dari user '$TARGET_EMAIL'."
elif [ "$HTTP_STATUS" -eq 403 ]; then
    echo "❌ GAGAL (403 Forbidden): Pastikan admin Anda memiliki hak 'manage-users'."
else
    echo "❌ GAGAL: Terjadi kesalahan. HTTP Status: $HTTP_STATUS"
fi
echo "========================================="
