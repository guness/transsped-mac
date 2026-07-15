#!/usr/bin/env bash
# Wraps a built "TransSped.app" into a distributable DMG (with an
# Applications drop-target), and optionally notarizes + staples it.
#
#   ./scripts/build-app.sh          # build the .app first (SIGN_ID for a Developer ID build)
#   ./scripts/make-dmg.sh           # -> TransSped.dmg
#   AC_PROFILE=<profile> ./scripts/make-dmg.sh   # also notarize + staple
#
# Notarization requires a Developer ID-signed, hardened-runtime app (build with
# SIGN_ID set) and stored notarytool credentials. See docs/PACKAGING.md.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="TransSped.app"
DMG="TransSped.dmg"
VOL="TransSped"

[ -d "$APP" ] || { echo "error: '$APP' not found — run ./scripts/build-app.sh first" >&2; exit 1; }

echo "==> staging DMG contents"
STAGING="$(mktemp -d)"
cp -R "$APP" "$STAGING/"
ln -s /Applications "$STAGING/Applications"

echo "==> creating $DMG"
rm -f "$DMG"
hdiutil create -volname "$VOL" -srcfolder "$STAGING" -ov -format UDZO "$DMG" >/dev/null
rm -rf "$STAGING"

if [ -n "${AC_PROFILE:-}" ]; then
  echo "==> notarizing (keychain profile: $AC_PROFILE)"
  xcrun notarytool submit "$DMG" --keychain-profile "$AC_PROFILE" --wait
  echo "==> stapling ticket"
  xcrun stapler staple "$DMG"
  xcrun stapler staple "$APP" || true
  echo "notarized + stapled: $DMG"
else
  echo "note: DMG is not notarized. On another Mac it will show a Gatekeeper"
  echo "      warning unless notarized. To notarize: build with SIGN_ID set, then"
  echo "      AC_PROFILE=<profile> ./scripts/make-dmg.sh  (see docs/PACKAGING.md)."
fi
echo "built: $DMG"
