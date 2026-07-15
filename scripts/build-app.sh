#!/usr/bin/env bash
# Builds "EasySign for Mac.app" — a double-clickable setup app that fetches the
# Trans Sped cloud cert and registers the PKCS#11 module into the user's normal
# Firefox profile (no dedicated profile, no TLS pin, no disabling of existing
# certs). Run once; then use Firefox as usual for ANAF.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="EasySign for Mac.app"
ARCH="${GOARCH:-arm64}"

echo "==> building PKCS#11 module (libtscloud-pkcs11.dylib)"
CGO_ENABLED=1 GOARCH="$ARCH" go build -buildmode=c-shared -o libtscloud-pkcs11.dylib ./cmd/pkcs11/
codesign -s - libtscloud-pkcs11.dylib

echo "==> assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
GOARCH="$ARCH" go build -o "$APP/Contents/MacOS/tscloud-app" ./cmd/tscloud-app/
cp libtscloud-pkcs11.dylib "$APP/Contents/Resources/"

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>EasySign for Mac</string>
  <key>CFBundleDisplayName</key><string>EasySign for Mac</string>
  <key>CFBundleIdentifier</key><string>ro.transsped.easysign-mac</string>
  <key>CFBundleVersion</key><string>1.0</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleExecutable</key><string>tscloud-app</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

codesign --force --deep -s - "$APP" 2>/dev/null || true
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true

echo "built: $APP  (double-click it, or run: open \"$APP\")"
