#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
CGO_ENABLED=1 GOARCH=arm64 go build -buildmode=c-shared -o libtscloud-pkcs11.dylib ./cmd/pkcs11/
codesign -s - libtscloud-pkcs11.dylib
go build -o tscloud-setup ./cmd/tscloud-setup/
echo "built: libtscloud-pkcs11.dylib, tscloud-setup"
