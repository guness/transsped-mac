#!/usr/bin/env bash
# Builds "TransSped.app" — a double-clickable setup app that fetches the
# Trans Sped cloud cert and registers the PKCS#11 module into the user's normal
# Firefox profile (no dedicated profile, no TLS pin, no disabling of existing
# certs). Run once; then use Firefox as usual for ANAF.
#
# Signing:
#   ad-hoc by default (fine for running locally on this Mac).
#   Set SIGN_ID to a "Developer ID Application: …" identity to produce a
#   hardened-runtime, timestamped build suitable for notarization + sharing:
#     SIGN_ID="Developer ID Application: Your Name (TEAMID)" ./scripts/build-app.sh
#   (list identities: security find-identity -v -p codesigning)
set -euo pipefail
cd "$(dirname "$0")/.."

APP="TransSped.app"
ARCH="${GOARCH:-arm64}"
SIGN_ID="${SIGN_ID:-}"

# sign <path> — Developer ID (hardened runtime + timestamp) when SIGN_ID is set,
# otherwise an ad-hoc signature.
sign() {
  if [ -n "$SIGN_ID" ]; then
    codesign --force --options runtime --timestamp -s "$SIGN_ID" "$1"
  else
    codesign --force -s - "$1"
  fi
}

echo "==> building PKCS#11 module (libtscloud-pkcs11.dylib)"
CGO_ENABLED=1 GOARCH="$ARCH" go build -buildmode=c-shared -o libtscloud-pkcs11.dylib ./cmd/pkcs11/

echo "==> assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
GOARCH="$ARCH" go build -o "$APP/Contents/MacOS/tscloud-app" ./cmd/tscloud-app/
cp libtscloud-pkcs11.dylib "$APP/Contents/Resources/"

echo "==> rendering app icon (AppIcon.icns)"
ICONSET="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$ICONSET"
MASTER="$(mktemp -d)/icon_1024.png"
go run scripts/gen-icon.go "$MASTER"
for spec in "16:16x16" "32:16x16@2x" "32:32x32" "64:32x32@2x" \
            "128:128x128" "256:128x128@2x" "256:256x256" "512:256x256@2x" \
            "512:512x512" "1024:512x512@2x"; do
  px="${spec%%:*}"; name="${spec##*:}"
  sips -z "$px" "$px" "$MASTER" --out "$ICONSET/icon_${name}.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>TransSped</string>
  <key>CFBundleDisplayName</key><string>TransSped</string>
  <key>CFBundleIdentifier</key><string>ro.transsped.macos</string>
  <key>CFBundleVersion</key><string>1.0</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleExecutable</key><string>tscloud-app</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

echo "==> codesigning ($([ -n "$SIGN_ID" ] && echo "$SIGN_ID" || echo "ad-hoc"))"
# Sign inner-out: nested code first, then the bundle.
sign "$APP/Contents/Resources/libtscloud-pkcs11.dylib"
sign "$APP/Contents/MacOS/tscloud-app"
sign "$APP"
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true

echo "built: $APP  (double-click it, or run: open \"$APP\")"
if [ -z "$SIGN_ID" ]; then
  echo "note: ad-hoc signed — opens on THIS Mac. To share, see docs/PACKAGING.md."
fi
