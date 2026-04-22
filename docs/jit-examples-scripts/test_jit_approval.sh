#!/bin/bash

# ==========================================
# Knox JIT API Testing Script
# ==========================================

# 1) Konfigurasi
# Ganti URL ini sesuai dengan environment Knox Anda (misal: http://localhost:8080 atau https://knox.domain.com)
KNOX_URL="http://localhost:8080" 

# Masukkan API Key Knox Anda (Harus sama dengan Environment Variable KNOX_API_KEY di server Knox)
KNOX_API_KEY="YOUR_SECRET_API_KEY"

# Validasi jumlah argumen
if [ "$#" -ne 2 ]; then
    echo "Penggunaan: $0 <TICKET_ID> <STATUS_ATAU_AKSI>"
    echo "Contoh Update : $0 550e8400-e29b-41d4-a716-446655440000 approved"
    echo "Contoh Lihat  : $0 550e8400-e29b-41d4-a716-446655440000 print"
    exit 1
fi

TICKET_ID=$1
ACTION=$2

# Validasi input
if [[ "$ACTION" != "approved" && "$ACTION" != "rejected" && "$ACTION" != "print" ]]; then
    echo "Error: Argumen ke-2 hanya boleh 'approved', 'rejected', atau 'print'."
    exit 1
fi

echo "=========================================="
echo "Mengecek Data & Status untuk Tiket ${TICKET_ID}..."
echo "=========================================="
curl -s -X GET "${KNOX_URL}/knox-api/admin/tickets/${TICKET_ID}" \
     -H "X-Knox-API-Key: ${KNOX_API_KEY}"
echo -e "\n\n"

# Jika aksinya hanya print, maka keluar dari skrip di sini
if [ "$ACTION" == "print" ]; then
    echo "Berhasil menampilkan data tiket. Script dihentikan."
    exit 0
fi

echo "=========================================="
echo "Memproses update tiket $TICKET_ID menjadi: $ACTION ..."
echo "=========================================="

# 2) Eksekusi API Call PATCH
curl -X PATCH "${KNOX_URL}/knox-api/admin/tickets/${TICKET_ID}/status" \
     -H "X-Knox-API-Key: ${KNOX_API_KEY}" \
     -H "Content-Type: application/json" \
     -d "{
           \"status\": \"${ACTION}\",
           \"reason\": \"Uji coba menggunakan bash script\",
           \"updated_by\": \"admin-tester\"
         }"

echo -e "\n\nSelesai mengeksekusi request!"
