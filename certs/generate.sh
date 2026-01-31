#!/usr/bin/env bash
# Generate self-signed test certificates for development/testing.
# These certificates are NOT suitable for production use.

set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Generating self-signed CA and server certificate in ${DIR} ..."

# Generate CA key and certificate
openssl req -x509 -nodes -days 365 \
    -newkey rsa:2048 \
    -keyout "${DIR}/ca.key" \
    -out "${DIR}/ca.crt" \
    -subj "/CN=godiam-test-ca"

# Generate server key and CSR
openssl req -nodes -newkey rsa:2048 \
    -keyout "${DIR}/server.key" \
    -out "${DIR}/server.csr" \
    -subj "/CN=server.example.com"

# Sign server certificate with CA
openssl x509 -req -days 365 \
    -in "${DIR}/server.csr" \
    -CA "${DIR}/ca.crt" \
    -CAkey "${DIR}/ca.key" \
    -CAcreateserial \
    -out "${DIR}/server.crt"

rm -f "${DIR}/server.csr" "${DIR}/ca.srl"

echo "Done. Generated:"
echo "  ${DIR}/ca.key"
echo "  ${DIR}/ca.crt"
echo "  ${DIR}/server.key"
echo "  ${DIR}/server.crt"
