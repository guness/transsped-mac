# Cryptoki smoke test

Proves `libtscloud-pkcs11.dylib` loads in a real PKCS#11 host
(`pkcs11-tool` from OpenSC) and correctly vends its certificate/key objects,
without needing the real Trans Sped cloud account or a live OTP. Uses a
throwaway, locally-generated self-signed certificate — no real credentials
involved.

## Prerequisites

- `./scripts/build.sh` has been run, producing `libtscloud-pkcs11.dylib` in
  the repo root.
- OpenSC's `pkcs11-tool` is installed (`brew install opensc`).

## Procedure

```bash
# 1. Throwaway config dir (never the real ~/.config/tscloud)
SMOKE=$(mktemp -d)

# 2. Throwaway self-signed RSA-2048 cert, DER-encoded, as leaf.der
openssl req -x509 -newkey rsa:2048 -nodes -keyout /dev/null -outform DER \
  -out "$SMOKE/leaf.der" -days 1 -subj "/CN=Smoke Test"

# 3. Minimal config.json (baseURL is a black hole — --list-objects never
#    calls it; only --sign would)
cat > "$SMOKE/config.json" <<'EOF'
{"baseURL":"https://example.invalid/","userID":"smoke","credentialID":"smoke","label":"Smoke Test"}
EOF

# 4. List objects through the built module.
#    NOTE: pkcs11-tool on macOS is a hardened binary and refuses to
#    dlopen() a relative path ("relative path not allowed in hardened
#    program") — pass an ABSOLUTE path to --module.
TSCLOUD_DIR="$SMOKE" pkcs11-tool \
  --module "$(pwd)/libtscloud-pkcs11.dylib" \
  --list-objects

# 5. Clean up
rm -rf "$SMOKE"
```

If nothing lists or the module errors out, re-run with `P11MOD_TRACE=1` set
to see the individual Cryptoki calls pkcs11mod is receiving/returning, e.g.:

```bash
P11MOD_TRACE=1 TSCLOUD_DIR="$SMOKE" pkcs11-tool \
  --module "$(pwd)/libtscloud-pkcs11.dylib" --list-objects
```

`--sign` is intentionally **not** exercised here — signing goes through the
real CSC API and triggers a live OTP approval, which requires the real
Trans Sped cloud account. That step belongs to the user, per the task brief
(see `scripts/build.sh` step 3 in `task-11-brief.md`).

## Actual run — 2026-07-14

Build:

```
$ ./scripts/build.sh
# tscloud/cmd/pkcs11
ld: warning: ignoring duplicate libraries: '-lpkcs11_exported'
built: libtscloud-pkcs11.dylib, tscloud-setup
```

(The `-lpkcs11_exported` warning is known/benign — vendored `pkcs11mod`
links its C shim twice; the resulting dylib is correct and exports
`_C_GetFunctionList` / `_C_Initialize` as verified with `nm -gU`.)

codesign:

```
$ codesign -dv libtscloud-pkcs11.dylib
Executable=/Users/guness/Desktop/EasySign-macos/libtscloud-pkcs11.dylib
Identifier=libtscloud-pkcs11-55554944b70a414a8d043a45e4c05076404f2bb8
Format=Mach-O thin (arm64)
CodeDirectory v=20400 size=12851 flags=0x2(adhoc) hashes=395+2 location=embedded
Signature=adhoc
Info.plist=not bound
TeamIdentifier=not set
Sealed Resources=none
Internal requirements count=0 size=12
```

Smoke test (`pkcs11-tool --list-objects` against the throwaway config):

```
$ TSCLOUD_DIR="$SMOKE" pkcs11-tool --module "$(pwd)/libtscloud-pkcs11.dylib" --list-objects
Using slot 0 with a present token (0x0)
Certificate Object; type = X.509 cert
  label:      Trans Sped Cloud
  subject:    DN: CN=Smoke Test
  serial:     3AFC5AEDED534EB186D44027C1FF721D1B6FA662
  ID:         b5:0f:8b:bd:08:fa:2d:c0:fe:62:db:17:73:74:07:83:db:d2:cf:72
  uri:        pkcs11:model=;manufacturer=Trans%20Sped;serial=;token=Trans%20Sped%20Cloud;id=%b5%0f%8b%bd%08%fa%2d%c0%fe%62%db%17%73%74%07%83%db%d2%cf%72;object=Trans%20Sped%20Cloud;type=cert
Private Key Object; RSA  2048 bits
  Modulus:    a41b53b739f68dbc850a78ded127d0565541665a35d21a7ed75296c69ec0474c
              b85634af2408f19407ce2e454618c700f162ab5cc8ce9964c3e9283fffffbfd9
              baa88af33038453bb1bfbe4cade14eec0de6a5944240fd904f23584b36137746
              7ddd004b9027c079fd20bccc159fc8494c34928a1c257767f1809a791b02f789
              79a5890354bdd9d619ef9f71f9d08e083ef25b2025de91d7b67be2ca0a099ec7
              8628a7fa8017ce242de4c7114adb66a2eac1ac686583d3bfa6b7cb08bfbb48c9
              2a59d86346997c0647088f390ca7ca724a4387010ed5d19deb5c28d343b53d22
              6ead0ebcce63c14d979bd0947cb27a4d02516f2d45b879e729fe0a662762d385
  Public exp: 65537 (0x010001)
  label:      Trans Sped Cloud
  ID:         b5:0f:8b:bd:08:fa:2d:c0:fe:62:db:17:73:74:07:83:db:d2:cf:72
  Usage:      sign
  Access:     none
  uri:        pkcs11:model=;manufacturer=Trans%20Sped;serial=;token=Trans%20Sped%20Cloud;id=%b5%0f%8b%bd%08%fa%2d%c0%fe%62%db%17%73%74%07%83%db%d2%cf%72;object=Trans%20Sped%20Cloud;type=private
```

**Result: PASS.** One Certificate Object and one Private Key Object were
listed, both labeled "Trans Sped Cloud" with matching `CKA_ID`
(`b5:0f:...`), matching the `internal/token.BuildObjects` contract. The
module loads cleanly in a real Cryptoki host, `C_GetSlotList` /
`C_GetTokenInfo` / `C_FindObjectsInit` / `C_FindObjects` /
`C_GetAttributeValue` all round-trip correctly end to end through
`pkcs11mod`.

`P11MOD_TRACE=1` was not needed — the first attempt succeeded once an
**absolute** module path was used (see gotcha below).

### Gotcha found during this run

The very first attempt used a relative `--module ./libtscloud-pkcs11.dylib`
and failed:

```
sc_dlopen_deep failed: dlopen(./libtscloud-pkcs11.dylib, 0x0005): tried:
'./libtscloud-pkcs11.dylib' (relative path not allowed in hardened program), ...
error: Failed to load pkcs11 module
Aborting.
```

This is a macOS hardened-runtime restriction on the `pkcs11-tool` binary
itself (Homebrew/OpenSC build), not a bug in `libtscloud-pkcs11.dylib` —
`dlopen()` from a hardened process refuses relative paths. Passing an
absolute path (`"$(pwd)/libtscloud-pkcs11.dylib"`) resolved it. The
procedure above already uses the absolute form.
