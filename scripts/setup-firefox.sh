#!/usr/bin/env bash
# Sets up a DEDICATED Firefox profile for the ANAF/Trans Sped Cloud PKCS#11
# module: creates the profile, loads libtscloud-pkcs11.dylib into its NSS
# database, pins TLS to 1.2 only, and imports any local intermediate
# certificate(s). Intended to be run by hand, by the end user, against
# their own machine -- it is NOT exercised in CI/automated testing against
# a real profile (see scripts/firefox-setup.md for how the underlying NSS
# module-load mechanism was validated on throwaway databases instead).
#
# Usage:
#   brew install nss   # provides modutil/certutil
#   ./scripts/setup-firefox.sh
set -euo pipefail

FF="/Applications/Firefox.app/Contents/MacOS/firefox"
PROFILE_NAME="anaf"
PROFILE_DIR="$HOME/.tscloud-firefox"

# Resolve the dylib path absolutely: NSS (like OpenSC's pkcs11-tool, see
# scripts/smoke-test.md) refuses -libfile paths that dlopen() can't resolve
# once its cwd changes, and hardened Firefox binaries reject relative
# dlopen paths outright.
DYLIB="$(cd "$(dirname "$0")/.." && pwd)/libtscloud-pkcs11.dylib"

if [ ! -x "$FF" ]; then
  echo "ERROR: Firefox not found at $FF -- install it from https://www.mozilla.org/firefox/ first." >&2
  exit 1
fi
if [ ! -f "$DYLIB" ]; then
  echo "ERROR: $DYLIB not found -- run ./scripts/build.sh first." >&2
  exit 1
fi
if ! command -v modutil >/dev/null 2>&1 || ! command -v certutil >/dev/null 2>&1; then
  echo "ERROR: modutil/certutil not found -- run: brew install nss" >&2
  exit 1
fi

echo "==> [1/4] Creating dedicated Firefox profile \"$PROFILE_NAME\" at $PROFILE_DIR"
"$FF" -CreateProfile "$PROFILE_NAME $PROFILE_DIR" -no-remote || true
mkdir -p "$PROFILE_DIR"

# -CreateProfile lays down the profile directory (prefs.js etc.) but does
# not necessarily initialize the NSS softoken database (cert9.db/key4.db)
# -- that's normally created lazily the first time Firefox itself opens
# the profile. Make sure it exists up front so modutil has a database to
# add the module to, whether or not Firefox has ever launched this profile.
if [ ! -e "$PROFILE_DIR/cert9.db" ] && [ ! -e "$PROFILE_DIR/cert8.db" ]; then
  echo "==> [2/4] Initializing NSS database in $PROFILE_DIR"
  certutil -N -d "sql:$PROFILE_DIR" --empty-password
else
  echo "==> [2/4] NSS database already present in $PROFILE_DIR"
fi

echo "==> [3/4] Loading the TransSpedCloud PKCS#11 module"
modutil -dbdir "sql:$PROFILE_DIR" -add "TransSpedCloud" -libfile "$DYLIB" -force

echo "==> [4/4] Pinning TLS 1.2 and importing intermediate certificate(s)"
# security.tls.version.{min,max} = 3 means TLS 1.2 (0=1.0, 1=1.1, 2=... no:
# NSS_SSL_LIBRARY_VERSION offsets from TLS1.0=1, so 3 == TLS 1.2, matching
# ANAF's cloud signing endpoints, which require TLS 1.2 and reject 1.3).
touch "$PROFILE_DIR/user.js"
if ! grep -q 'security.tls.version.min' "$PROFILE_DIR/user.js" 2>/dev/null; then
  cat >> "$PROFILE_DIR/user.js" <<'EOF'
user_pref("security.tls.version.min", 3);
user_pref("security.tls.version.max", 3);
EOF
fi

for f in "$HOME/.config/tscloud"/intermediate*.der; do
  [ -e "$f" ] || continue
  certutil -A -d "sql:$PROFILE_DIR" -n "TransSped Intermediate ($(basename "$f"))" -t ",," -i "$f" || true
done

echo "Profile ready. Launch:  $FF -profile $PROFILE_DIR -no-remote"
