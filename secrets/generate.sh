#!/usr/bin/env bash
# Generate RSA-4096 key pair for JWT RS256 signing.
# Run once: ./secrets/generate.sh
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ -f "$DIR/jwt_private.pem" && -f "$DIR/jwt_public.pem" ]]; then
  echo "Keys already exist — skipping generation."
  exit 0
fi

echo "Generating RSA-4096 key pair..."
openssl genrsa -out "$DIR/jwt_private.pem" 4096
openssl rsa -in "$DIR/jwt_private.pem" -pubout -out "$DIR/jwt_public.pem"
chmod 600 "$DIR/jwt_private.pem"
chmod 644 "$DIR/jwt_public.pem"
echo "Done: jwt_private.pem  jwt_public.pem"
