#!/usr/bin/env bash
# End-to-end proof that the compiled PKCS#11 module's C_Sign works through
# the real C ABI, headlessly, against a local mock CSC server:
#
#   pkcs11-tool -> dlopen(libtscloud-pkcs11.dylib) -> C_SignInit/C_Sign
#   -> Backend.Sign -> csc.Signer -> HTTP -> test/cscmock -> signature
#   -> back through the C ABI -> openssl verifies it against the cert.
#
# See scripts/sign-test.md for the documented procedure and a captured run.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> [1/6] Building dylib (./scripts/build.sh)"
./scripts/build.sh

WORK=$(mktemp -d)
MOCK_PID=""

cleanup() {
  if [ -n "$MOCK_PID" ]; then
    kill "$MOCK_PID" 2>/dev/null || true
    wait "$MOCK_PID" 2>/dev/null || true
  fi
  # `go run` compiles-then-execs a child binary; belt-and-suspenders in case
  # the child outlived the `go run` parent process.
  pkill -f "cscmock -key $WORK/key.pem" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "==> [2/6] Generating throwaway RSA-2048 key + self-signed cert in $WORK"
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$WORK/key.pem" -outform DER \
  -out "$WORK/leaf.der" -days 1 -subj "/CN=Sign Test"

echo "==> [3/6] Writing $WORK/config.json (leaf.der already in place)"
cat > "$WORK/config.json" <<'EOF'
{"baseURL":"http://127.0.0.1:8099/","userID":"t","credentialID":"t","label":"Sign Test"}
EOF

echo "==> [4/6] Starting mock CSC server on 127.0.0.1:8099"
go run ./test/cscmock -key "$WORK/key.pem" -addr 127.0.0.1:8099 &
MOCK_PID=$!

ready=0
for _ in $(seq 1 50); do
  if curl -s -o /dev/null "http://127.0.0.1:8099/credentials/sendOTP" -X POST -d '{}'; then
    ready=1
    break
  fi
  sleep 0.1
done
if [ "$ready" -ne 1 ]; then
  echo "FAIL: mock CSC server never came up on 127.0.0.1:8099" >&2
  exit 1
fi
echo "    mock CSC server is up (pid $MOCK_PID)"

echo "==> [5/6] Signing a random 32-byte digest through the compiled module"
head -c 32 /dev/urandom > "$WORK/digest.bin"

TSCLOUD_OTP=000000 TSCLOUD_DIR="$WORK" pkcs11-tool \
  --module "$(pwd)/libtscloud-pkcs11.dylib" \
  --login --pin 1234 \
  --sign --mechanism RSA-PKCS \
  --input-file "$WORK/digest.bin" \
  --output-file "$WORK/sig.bin"

echo "==> [6/6] Verifying the signature against the cert's public key"
openssl x509 -inform der -in "$WORK/leaf.der" -pubkey -noout > "$WORK/pub.pem"

if openssl pkeyutl -verify -pubin -inkey "$WORK/pub.pem" \
    -sigfile "$WORK/sig.bin" -in "$WORK/digest.bin" \
    -pkeyopt rsa_padding_mode:pkcs1 -pkeyopt digest:sha256; then
  echo "PASS: C_Sign end-to-end signature verified against the leaf certificate."
else
  echo "FAIL: signature verification failed." >&2
  exit 1
fi
