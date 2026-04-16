# Keycloak Realm Export Guide

Panduan untuk mengekspor Keycloak realm configuration yang akan digunakan untuk automated testing di CI pipeline.

---

## Langkah-Langkah Export

### 1. Login ke Keycloak Admin Console

Buka browser dan navigasi ke:
```
https://your-keycloak.domain.com/admin/
```
Login dengan admin credentials.

### 2. Pilih Realm

Pada dropdown di pojok kiri atas, pilih realm yang digunakan oleh Knox (misalnya `corp` atau `production`).

### 3. Partial Export

1. Navigasi ke **Realm Settings** (menu kiri)
2. Klik dropdown **Action** (pojok kanan atas)
3. Pilih **Partial export**
4. Aktifkan opsi berikut:
   - ✅ **Export groups and group memberships**
   - ✅ **Export clients**
   - ✅ **Export realm roles**
5. Klik **Export**
6. File JSON akan terdownload

> **Note:** Partial export **TIDAK** mengekspor user passwords. Kita akan membuat test users secara manual di file JSON.

### 4. Bersihkan Data Sensitif

Buka file JSON yang sudah diexport dan hapus/replace data sensitif:

```bash
# Replace nama realm untuk testing
sed -i 's/"realm": "corp"/"realm": "knox-test"/g' realm-export.json

# Replace client secret
sed -i 's/YOUR_PRODUCTION_CLIENT_SECRET/knox-test-secret/g' realm-export.json

# Replace client ID jika berbeda
sed -i 's/"clientId": "n8n-proxy"/"clientId": "knox-test"/g' realm-export.json

# Replace redirect URIs
sed -i 's|https://your-production-domain.com|http://localhost:8443|g' realm-export.json

# Replace backchannel logout URL
sed -i 's|http://knox:8443|http://knox-proxy:8443|g' realm-export.json

# Replace group prefix
sed -i 's/n8n-prod-/n8n-test-/g' realm-export.json
```

### 5. Tambahkan Test Users

Tambahkan array `users` ke dalam file JSON. Berikut template yang harus ditambahkan:

```json
{
  "users": [
    {
      "username": "testuser",
      "enabled": true,
      "email": "testuser@knox-ci.local",
      "emailVerified": true,
      "firstName": "Test",
      "lastName": "User",
      "credentials": [
        {
          "type": "password",
          "value": "testpass",
          "temporary": false
        }
      ],
      "groups": ["/n8n-test-testteam"],
      "clientRoles": {
        "knox-test": ["run:test-workflow-1"]
      }
    },
    {
      "username": "restricteduser",
      "enabled": true,
      "email": "restricted@knox-ci.local",
      "emailVerified": true,
      "firstName": "Restricted",
      "lastName": "User",
      "credentials": [
        {
          "type": "password",
          "value": "testpass",
          "temporary": false
        }
      ],
      "groups": ["/n8n-test-testteam"],
      "clientRoles": {
        "knox-test": []
      }
    },
    {
      "username": "adminuser",
      "enabled": true,
      "email": "admin@knox-ci.local",
      "emailVerified": true,
      "firstName": "Admin",
      "lastName": "User",
      "credentials": [
        {
          "type": "password",
          "value": "testpass",
          "temporary": false
        }
      ],
      "groups": ["/n8n-test-testteam"],
      "clientRoles": {
        "knox-test": ["run:*", "edit:*"]
      }
    }
  ]
}
```

### 6. Pastikan Group Exists

Tambahkan group di bagian `groups` jika belum ada:

```json
{
  "groups": [
    {
      "name": "n8n-test-testteam",
      "path": "/n8n-test-testteam"
    }
  ]
}
```

### 7. Pastikan Client Roles Exists

Di bagian `roles.client`, tambahkan roles yang dibutuhkan:

```json
{
  "roles": {
    "client": {
      "knox-test": [
        {"name": "run:*", "description": "Run all workflows"},
        {"name": "edit:*", "description": "Edit all workflows"},
        {"name": "run:test-workflow-1", "description": "Run test workflow 1"},
        {"name": "edit:test-workflow-1", "description": "Edit test workflow 1"}
      ]
    }
  }
}
```

### 8. Pastikan Protocol Mappers Ada

Di bagian client configuration, pastikan ada mapper `groups` dan `client-roles`:

```json
{
  "protocolMappers": [
    {
      "name": "groups",
      "protocol": "openid-connect",
      "protocolMapper": "oidc-group-membership-mapper",
      "config": {
        "full.path": "true",
        "id.token.claim": "true",
        "access.token.claim": "true",
        "claim.name": "groups",
        "userinfo.token.claim": "true"
      }
    }
  ]
}
```

### 9. Simpan File

Simpan file ke lokasi berikut dalam repository:

```
tests/keycloak/knox-realm.json
```

### 10. Verifikasi

Jalankan secara lokal untuk memverifikasi import berhasil:

```bash
cd tests
docker compose -f docker-compose.test.yaml up keycloak -d

# Tunggu Keycloak ready
sleep 30

# Verifikasi realm exists
curl -sf http://localhost:8080/realms/knox-test/.well-known/openid-configuration | jq .issuer

# Expected: "http://localhost:8080/realms/knox-test"
```

---

## Alternatif: Gunakan Template Bawaan

Jika Anda tidak memiliki realm untuk diexport, file `tests/keycloak/knox-realm.json` sudah berisi template default yang siap digunakan. Template ini dibuat berdasarkan [keycloak-setup-guide.md](./keycloak-setup-guide.md) dan mencakup:

- Client `knox-test` (confidential, standard flow, backchannel logout)
- Group `/n8n-test-testteam`
- Client roles: `run:*`, `edit:*`, `run:test-workflow-1`, `edit:test-workflow-1`
- 3 Test users: `testuser`, `restricteduser`, `adminuser`

---

## Troubleshooting

### "Realm already exists"
Keycloak tidak akan mengimport realm jika sudah ada. Hapus volume Docker:
```bash
docker compose -f docker-compose.test.yaml down -v
```

### "Client secret mismatch"
Pastikan `secret` di realm JSON sama persis dengan `OIDC_CLIENT_SECRET` di docker-compose.test.yaml.

### "Groups not mapped to token"
Pastikan protocol mapper `groups` ada di client configuration dengan `id.token.claim` = `true`.
