#!/usr/bin/env bash
# Builds "TransSped.app" — a native SwiftUI window (Contents/MacOS/TransSped)
# driving the headless Go engine (Contents/Resources/tscloud-engine) and the
# PKCS#11 module (Contents/Resources/libtscloud-pkcs11.dylib).
#
# Signing: ad-hoc by default; set SIGN_ID to a "Developer ID Application: …"
# identity (or its SHA-1) for a hardened-runtime, timestamped, notarizable build.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="TransSped.app"
ARCH="${GOARCH:-arm64}"
SIGN_ID="${SIGN_ID:-}"
TARGET="arm64-apple-macos13"

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

echo "==> building Go engine (tscloud-engine)"
GOARCH="$ARCH" go build -o "$APP/Contents/Resources/tscloud-engine" ./cmd/tscloud-engine/
cp libtscloud-pkcs11.dylib "$APP/Contents/Resources/"

echo "==> compiling SwiftUI app (TransSped)"
swiftc -O -parse-as-library -target "$TARGET" \
  -framework SwiftUI -framework AppKit \
  -o "$APP/Contents/MacOS/TransSped" app/*.swift

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
  <key>CFBundleVersion</key><string>0.0.2</string>
  <key>CFBundleShortVersionString</key><string>0.0.2</string>
  <key>CFBundleExecutable</key><string>TransSped</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

echo "==> codesigning ($([ -n "$SIGN_ID" ] && echo "$SIGN_ID" || echo "ad-hoc"))"
sign "$APP/Contents/Resources/libtscloud-pkcs11.dylib"
sign "$APP/Contents/Resources/tscloud-engine"
sign "$APP/Contents/MacOS/TransSped"
sign "$APP"
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true

echo "built: $APP  (double-click it, or run: open \"$APP\")"
if [ -z "$SIGN_ID" ]; then
  echo "note: ad-hoc signed — opens on THIS Mac. To share, see docs/PACKAGING.md."
fi
