# Firefox integration: setup + NSS load validation

`scripts/setup-firefox.sh` wires `libtscloud-pkcs11.dylib` into a
**dedicated** Firefox profile so the Trans Sped Cloud credential shows up in
Firefox's client-certificate picker for ANAF, without touching the user's
default/main profile.

## What the script does

1. Creates a dedicated profile named `anaf` at `~/.tscloud-firefox` via
   `firefox -CreateProfile "anaf $PROFILE_DIR" -no-remote`.
2. Ensures the profile has an initialized NSS database (`certutil -N` if
   `cert9.db`/`cert8.db` isn't already there — `-CreateProfile` lays down the
   profile directory but doesn't always initialize the softoken db ahead of
   Firefox's own first launch).
3. Loads the module: `modutil -dbdir "sql:$PROFILE_DIR" -add "TransSpedCloud"
   -libfile "<absolute path>/libtscloud-pkcs11.dylib" -force`. The dylib path
   is resolved absolutely from the script's own location, matching the
   `pkcs11-tool`/`modutil` requirement (both refuse relative `dlopen()` paths
   from a hardened binary).
4. Pins TLS to 1.2 only by appending `user_pref("security.tls.version.min",
   3)` / `user_pref("security.tls.version.max", 3)` to `user.js` (idempotent —
   skipped if already present), matching ANAF's cloud signing endpoints.
5. Imports any `~/.config/tscloud/intermediate*.der` via `certutil -A`.
6. Prints the launch command: `firefox -profile $PROFILE_DIR -no-remote`.

## Prerequisites

- `./scripts/build.sh` has been run, producing `libtscloud-pkcs11.dylib` in
  the repo root.
- `brew install nss` (provides `modutil`/`certutil`; the script checks for
  both and fails fast with that instruction if missing).
- Firefox installed at `/Applications/Firefox.app`.

## Running it

```bash
brew install nss
chmod +x scripts/setup-firefox.sh   # already executable in the repo
./scripts/setup-firefox.sh
```

Expected final line: `Profile ready. Launch:  /Applications/Firefox.app/Contents/MacOS/firefox -profile /Users/<you>/.tscloud-firefox -no-remote`.

Verify: `modutil -dbdir "sql:$HOME/.tscloud-firefox" -list` shows
`TransSpedCloud`.

This script is meant to be run by hand against the real Firefox install —
it was **not** executed against a real profile as part of this task (that
would create/mutate the developer's actual `profiles.ini` and NSS
databases). Instead, the underlying load mechanism (`modutil -add` /
`modutil -list` against an NSS `sql:` database, which is exactly what
`-CreateProfile` + `modutil -add` do internally) was validated end-to-end
against throwaway databases, below.

## NSS load validation (throwaway databases only)

Goal: prove `libtscloud-pkcs11.dylib` loads cleanly through NSS's own
module-loading code path (`modutil`/`SECMOD_LoadPKCS11Module`, the same
code Firefox uses to load `pkcs11.txt`/`secmod.db` entries at startup) in
both the empty-backend state (config load failed) and the populated state
(real leaf cert + key present) — **without crashing** in either case. This
is the actual Firefox-integration risk Task 13 exists to catch: a bad
module load must never take the browser down with it.

Environment: `nss 3.125` / `nspr 4.39` via Homebrew (`modutil`/`certutil`
were not previously installed on this machine; installed with
`brew install nss` for this task). `pkcs11-tool` (OpenSC) already present
from Task 11/12's smoke test.

### Bug found and fixed during validation: `CK_INFO.cryptokiVersion` was zero

The first `modutil -add` attempt against the (then-unfixed)
`libtscloud-pkcs11.dylib` failed:

```
$ TSCLOUD_DIR="$EMPTYCFG" modutil -dbdir "sql:$TMPDB" -add "TransSpedCloud" -libfile "$(pwd)/libtscloud-pkcs11.dylib" -force
ERROR: Failed to add module "TransSpedCloud". Probable cause : "dlsym(0x745dac90, C_GetInterface): symbol not found".
```

This looked like NSS 3.125 requiring the PKCS#11 v3.0 `C_GetInterface`
entrypoint (which the vendored `pkcs11mod` dependency does not implement —
it only implements legacy v2.x `C_GetFunctionList`). That turned out to be
a red herring: a hand-built minimal C module that *also* only implements
`C_GetFunctionList` (no `C_GetInterface`) loaded into `modutil` without any
issue, proving NSS 3.125 correctly falls back to v2.x modules and does not
require `C_GetInterface`.

Driving the real dylib manually (`dlopen` → `C_GetFunctionList` →
`C_Initialize` → `C_GetInfo`, replicating what NSS's loader does) showed the
actual defect: `C_GetInfo()` returned `cryptokiVersion = 0.0`. Reading
`internal/token/backend.go`'s `Backend.GetInfo()`:

```go
func (b *Backend) GetInfo() (pkcs11.Info, error) {
	return pkcs11.Info{ManufacturerID: "Trans Sped", LibraryDescription: "TS Cloud PKCS#11"}, nil
}
```

`pkcs11.Info.CryptokiVersion` (and `LibraryVersion`) were never set, so both
defaulted to the Go zero value `{0, 0}`. `pkcs11mod`'s C shim copies this
struct verbatim into `CK_INFO` (`pInfo->cryptokiVersion = goInfo.cryptokiVersion`
in `pkcs11_exported.c`), so NSS received a library reporting Cryptoki
version 0.0 — which it treats as invalid/unsupported and rejects, while its
diagnostic path happens to surface the earlier (harmless) `C_GetInterface`
probe failure as the "probable cause" instead of the real reason. This was
a genuine module-load bug that would have silently blocked Firefox from
ever loading `libtscloud-pkcs11.dylib` at all, in any state.

**Fix** (`internal/token/backend.go`, `Backend.GetInfo`): set
`CryptokiVersion: pkcs11.Version{Major: 2, Minor: 20}` and
`LibraryVersion: pkcs11.Version{Major: 1, Minor: 0}`. After rebuilding, both
load tests below pass cleanly.

### [A] Empty-backend load test (config missing/unreadable)

Proves the `cmd/pkcs11/main.go` hardening (Part A: `pkcs11mod.SetBackend`
always called, even on `config.Load()` failure) results in a module NSS can
load without crashing, with no cert/key objects present.

```
$ TMPDB=$(mktemp -d)
$ certutil -N -d "sql:$TMPDB" --empty-password
$ EMPTYCFG=$(mktemp -d)   # deliberately empty -- config.Load() fails here
$ TSCLOUD_DIR="$EMPTYCFG" modutil -dbdir "sql:$TMPDB" -add "TransSpedCloud" \
    -libfile "$(pwd)/libtscloud-pkcs11.dylib" -force
Module "TransSpedCloud" added to database.

$ modutil -dbdir "sql:$TMPDB" -list
...
  2. TransSpedCloud
	library name: /Users/guness/Desktop/EasySign-macos/libtscloud-pkcs11.dylib
	   uri: pkcs11:library-manufacturer=Trans%20Sped;library-description=TS%20Cloud%20PKCS%2311;library-version=1.0
	 slots: 1 slot attached
	status: loaded

	 slot: TS Cloud
	token: Trans Sped Cloud
	  uri: pkcs11:token=Trans%20Sped%20Cloud;manufacturer=Trans%20Sped
-----------------------------------------------------------
```

**Result: PASS.** The module loads, registers its one slot/token
("TS Cloud" / "Trans Sped Cloud"), and lists cleanly with **no** cert/key
objects (config load failed, so `NewBackend(nil, &csc.Signer{})` served an
empty object list) — no crash, no hang, `P11MOD_TRACE=1` was not needed.

### [B] Populated-backend load test (real config present)

```
$ POPCFG=$(mktemp -d)
$ openssl req -x509 -newkey rsa:2048 -nodes -keyout /dev/null -outform DER \
    -out "$POPCFG/leaf.der" -days 1 -subj "/CN=Firefox Load Test"
$ cat > "$POPCFG/config.json" <<'EOF'
{"baseURL":"https://example.invalid/","userID":"ff","credentialID":"ff","label":"Firefox Load Test"}
EOF
$ TMPDB2=$(mktemp -d)
$ certutil -N -d "sql:$TMPDB2" --empty-password
$ TSCLOUD_DIR="$POPCFG" modutil -dbdir "sql:$TMPDB2" -add "TransSpedCloud" \
    -libfile "$(pwd)/libtscloud-pkcs11.dylib" -force
Module "TransSpedCloud" added to database.

$ modutil -dbdir "sql:$TMPDB2" -list
... (same "TransSpedCloud" entry as above) ...

$ TSCLOUD_DIR="$POPCFG" pkcs11-tool --module "$(pwd)/libtscloud-pkcs11.dylib" --list-token-slots
Available slots:
Slot 0 (0x0): TS Cloud
  token label        : Trans Sped Cloud
  token manufacturer : Trans Sped
  token model        :
  token flags        : login required, token initialized, PIN initialized
  hardware version   : 0.0
  firmware version   : 0.0
  serial num         :
  pin min/max        : 0/0
  uri                : pkcs11:model=;manufacturer=Trans%20Sped;serial=;token=Trans%20Sped%20Cloud
```

**Result: PASS.** Both NSS's own loader (`modutil`) and OpenSC's
`pkcs11-tool` see the module and its one slot/token cleanly with a real
config present. `pkcs11-tool --list-objects` (run separately, not shown
above — see `scripts/smoke-test.md` for the full object dump against this
exact dylib) additionally confirmed the certificate and private key objects
are vended correctly through this same NSS-compatible load path.

`P11MOD_TRACE=1` was available for deeper Cryptoki-call tracing but was not
needed for either test once the `CryptokiVersion` fix above was applied.

### Cleanup

All temp directories (`$TMPDB`, `$EMPTYCFG`, `$POPCFG`, `$TMPDB2`) were
removed with `rm -rf` after each test; the throwaway leaf cert/key were
never written outside those temp dirs. The user's real
`~/.tscloud-firefox` profile and `~/.config/tscloud` config were never
touched by this validation.

## Conclusion

**DONE** (not just DONE_WITH_CONCERNS): NSS loads `libtscloud-pkcs11.dylib`
cleanly in both the empty-backend and populated-backend states, through the
same code path Firefox itself uses to load PKCS#11 modules. The
`CryptokiVersion` bug found during validation was a real, load-blocking
defect (independent of the Part A entrypoint hardening) and has been fixed
in `internal/token/backend.go`.
