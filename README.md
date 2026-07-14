# tscloud — ANAF SPV login on macOS via Trans Sped Cloud

`tscloud` is a macOS **PKCS#11 module** that lets **Firefox** present your
**Trans Sped cloud qualified certificate** for client-certificate TLS, so you
can log in to **ANAF SPV** (`pfinternet.anaf.ro`) from a Mac — no Windows VM,
no Wine, no physical token.

On Windows, EasySign / the "Trans Sped Cloud KSP" makes the cloud-held
private key usable by the browser's TLS stack: when ANAF requests a client
certificate mid-handshake, Windows forwards the handshake hash to Trans
Sped's cloud, prompts for an OTP, and returns the signature. macOS has no
CNG/KSP layer, so `tscloud` replaces it with a PKCS#11 module loaded into
Firefox's NSS:

```
Firefox (dedicated profile, TLS pinned to 1.2)
  │  ANAF requests a client cert → NSS needs a CertificateVerify signature
  ▼
NSS ──C_Sign(DigestInfo(SHA-256, handshake hash))──▶  libtscloud-pkcs11.dylib
                                                          │ parse DigestInfo,
                                                          │ OTP dialog,
                                                          │ authorize + signHash
                                                          ▼
                                                    Trans Sped cloud (CSC API)
  ◀───────────────── RSA PKCS#1 v1.5 signature ──────────┘
```

`C_Sign` forwards the TLS handshake hash to Trans Sped's CSC cloud API
(`signatures/signHash`), i.e. it is the macOS equivalent of the Windows
"Cloud KSP". TLS is pinned to **1.2**: the cloud only produces RSA
PKCS#1 v1.5 signatures (no RSA-PSS), and TLS 1.3's CertificateVerify
requires PSS, so 1.3 client auth is impossible with this cloud.

**Scope:** interactive ANAF SPV login in Firefox only. See
[Scope / non-goals](#scope--non-goals) below.

## Prerequisites

- **Go 1.21+** (developed against 1.26; `go.mod` requires `go 1.26.3` toolchain)
- **Xcode Command Line Tools** (`xcode-select --install`) — provides `clang`/`cc`, needed for the cgo build
- `brew install nss opensc` — `nss` provides `modutil`/`certutil` (Firefox profile setup), `opensc` provides `pkcs11-tool` (testing/diagnostics)
- **Firefox** — required; Chrome and Safari use the macOS Keychain for client certs and have no PKCS#11 loader, so they cannot use this module
- An existing **Trans Sped cloud qualified certificate**, already enrolled with ANAF (form 150) — enrollment itself is out of scope, see below

## Build

```bash
./scripts/build.sh
```

Produces, in the repo root:
- `libtscloud-pkcs11.dylib` — the PKCS#11 module (arm64, ad-hoc codesigned)
- `tscloud-setup` — the one-time credential-fetch CLI

Neither binary is committed to git (see `.gitignore`); build them locally.

## One-time setup (order matters)

**1. Fetch your cloud certificate:**

```bash
./tscloud-setup -user "<your Trans Sped email or phone>"
```

This calls the Trans Sped CSC API (`credentials/list` → `credentials/info`)
and writes:

```
~/.config/tscloud/config.json        # baseURL, userID, credentialID, label
~/.config/tscloud/leaf.der           # your certificate, DER
~/.config/tscloud/intermediate0.der  # issuing CA chain (Trans Sped QCA), DER
~/.config/tscloud/intermediate1.der  # (if more than one)
```

On success it prints the `credentialID` and the account's `SCAL` value (see
[How it works](#how-it-works) below). If `credentials/list` comes back
empty, your certificate is probably on a different backend — see
[Troubleshooting in the runbook](docs/RUNBOOK.md).

**2. Set up the dedicated Firefox profile:**

```bash
./scripts/setup-firefox.sh
```

Creates `~/.tscloud-firefox` (a profile separate from your normal Firefox
profile, so these settings never affect everyday browsing), loads
`libtscloud-pkcs11.dylib` into its NSS database, pins
`security.tls.version.{min,max} = 3` (TLS 1.2 only) in `user.js`, and
imports any `~/.config/tscloud/intermediate*.der` so the client-cert chain
builds.

Run step 1 before step 2 — `setup-firefox.sh` imports intermediates from
`~/.config/tscloud`, which only exists after `tscloud-setup` has run.

## Daily use

```bash
/Applications/Firefox.app/Contents/MacOS/firefox -profile "$HOME/.tscloud-firefox" -no-remote
```

Then:
1. Go to `https://pfinternet.anaf.ro`.
2. Click **"Autentificare certificat"**.
3. Pick the **Trans Sped Cloud** certificate from the picker.
4. Approve the OTP prompt (native macOS dialog, from the Trans Sped OTP app
   or SMS).
5. The SPV dashboard loads.

## How it works

- **~1 OTP per login.** Your account has `SCAL = 2` — each signing
  authorization (SAD) is OTP-bound to one specific hash. A normal SPV login
  performs exactly one client-cert TLS handshake, so it costs one OTP,
  matching the existing Windows experience. See
  [docs/RUNBOOK.md](docs/RUNBOOK.md) for how to measure this on your first
  real login.
- **TLS must stay pinned to 1.2.** The cloud only ever produces RSA
  PKCS#1 v1.5 signatures (no RSA-PSS). TLS 1.3's CertificateVerify mandates
  PSS, so 1.3 client auth is impossible with this backend — hence
  `setup-firefox.sh` forces `security.tls.version.max = 3`. Do not change
  this setting in the `tscloud-firefox` profile.
- **Firefox is required.** Chrome and Safari get client certificates from
  the macOS Keychain and have no PKCS#11 module loader; only Firefox (via
  NSS) can load `libtscloud-pkcs11.dylib`.

## Testing

```bash
go build ./...
go test ./...
```

- `scripts/smoke-test.md` — procedure (+ a captured run) proving the built
  dylib loads in `pkcs11-tool` and correctly vends its certificate/key
  objects, using a throwaway self-signed cert (no real credentials needed).
- `scripts/sign-test.sh` — end-to-end proof that `C_Sign` works through the
  real C ABI: `pkcs11-tool --sign` against the compiled module, backed by a
  local mock CSC server (`test/cscmock`), with the resulting signature
  verified against the cert's public key via `openssl`. Run it directly:
  `./scripts/sign-test.sh`.
- `scripts/firefox-setup.md` — how the module's NSS load path (`modutil
  -add`) was validated on throwaway NSS databases, both with and without a
  saved credential.

None of the above touch your real Trans Sped account or `~/.config/tscloud`.

## Known gotchas

- **Vendored `pkcs11mod`.** The build depends on Namecoin's `pkcs11mod`,
  which requires a hand-generated `spec/` directory, `strings.go`, and a
  pre-compiled static archive `libpkcs11_exported.a` that its own upstream
  build process doesn't produce for a normal `go get`. These are vendored
  and committed under `vendor/github.com/namecoin/pkcs11mod/`. The
  committed `libpkcs11_exported.a` is a compiled **darwin/arm64** archive —
  if you ever build on a different architecture, regenerate it (recipe in
  `.superpowers/sdd/task-9-report.md`): re-run `go mod vendor`, reseed
  `spec/`/`strings.go`, then `cc -c pkcs11_exported.c -o pkcs11_exported.o
  && ar cru libpkcs11_exported.a pkcs11_exported.o` inside the vendored
  package directory. A bare `go mod vendor` wipes these hand-added files —
  re-apply the recipe after adding/upgrading any Go dependency.
- **`pkcs11-tool` needs an absolute `--module` path.** macOS's hardened
  runtime rejects a relative-path `dlopen()`; `pkcs11-tool --module
  ./libtscloud-pkcs11.dylib ...` fails with "relative path not allowed in
  hardened program". Always pass an absolute path, e.g. `--module
  "$(pwd)/libtscloud-pkcs11.dylib"`. `setup-firefox.sh` already resolves the
  dylib path absolutely for this reason.
- **No `.h` file is generated by the build.** `-buildmode=c-shared` only
  emits a public header for `//export` directives living in `package main`;
  `pkcs11mod`'s `//export` glue lives in the vendored package, not in
  `cmd/pkcs11`. This is expected — the dylib still correctly exports
  `_C_GetFunctionList`/`_C_Initialize` (verify with `nm -gU
  libtscloud-pkcs11.dylib`), which is all a Cryptoki host needs.

## Security

- The Trans Sped **signature PIN** is collected once via Firefox's own
  PKCS#11 PIN dialog at login (`C_Login`) and cached in memory for the
  session only — never written to disk.
- The **OTP** is collected per signature via a native `osascript` dialog —
  also never written to disk or logged.
- `~/.config/tscloud/` holds only your **public** certificate chain
  (`leaf.der`, `intermediate*.der`) and non-secret identifiers
  (`config.json`: base URL, user ID, credential ID, label). No private key
  material is ever stored locally — signing is delegated to the Trans Sped
  cloud on every call.

## Scope / non-goals

- **Interactive Firefox login only.** This is not a general PKCS#11
  signing library.
- **Not document signing** (PDF/PKCS#7) — that's EasySign's job on other
  platforms, not this module's.
- **Not certificate enrollment.** Your Trans Sped cloud certificate must
  already be enrolled with ANAF (form 150); do that on Windows or with your
  existing tooling first.
- **Not Chrome/Safari** — see [How it works](#how-it-works) above.

## Further reading

- [docs/RUNBOOK.md](docs/RUNBOOK.md) — step-by-step for your first real
  ANAF login, plus troubleshooting.
- [docs/specs/2026-07-14-anaf-macos-login-design.md](docs/specs/2026-07-14-anaf-macos-login-design.md)
  — the full design spec (architecture, CSC endpoint contracts, algorithm
  selection, risks).
