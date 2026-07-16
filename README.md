# tscloud — ANAF SPV login on macOS via Trans Sped Cloud

`tscloud` is a macOS **PKCS#11 module** that lets **Firefox** present your
**Trans Sped cloud qualified certificate** for client-certificate TLS, so you
can log in to **ANAF SPV** (`pfinternet.anaf.ro` / `app.anaf.ro`) from a Mac —
no Windows VM, no Wine, no physical token.

On Windows, EasySign / the "Trans Sped Cloud KSP" makes the cloud-held
private key usable by the browser's TLS stack: when ANAF requests a client
certificate mid-handshake, Windows forwards the handshake hash to Trans
Sped's cloud, prompts for PIN + OTP, and returns the signature. macOS has no
CNG/KSP layer, so `tscloud` replaces it with a PKCS#11 module loaded into
Firefox's NSS:

```
Firefox (your normal profile)
  │  ANAF's F5 BIG-IP APM requests a client cert (via TLS renegotiation)
  ▼
NSS ──C_Sign(DigestInfo(SHA-256, handshake hash))──▶  libtscloud-pkcs11.dylib
                                                          │ parse DigestInfo,
                                                          │ PIN + OTP dialogs,
                                                          │ authorize + signHash
                                                          ▼
                                                    Trans Sped cloud (CSC API)
  ◀───────────────── RSA PKCS#1 v1.5 signature ──────────┘
```

`C_Sign` forwards the TLS handshake hash to Trans Sped's CSC cloud API
(`signatures/signHash`), i.e. it is the macOS equivalent of the Windows
"Cloud KSP". The working ANAF portal (`app.anaf.ro`) runs on an **F5 BIG-IP
APM** that requests the client cert via TLS **renegotiation** and offers
`rsa_pkcs1_sha256`, which the cloud can sign.

**Scope:** interactive ANAF SPV login in Firefox only. See
[Scope / non-goals](#scope--non-goals) below.

## Install (the app)

1. **Quit Firefox** (a security module can't be added while it's running).
2. Open **`TransSped.app`** (double-click, or `open "TransSped.app"`). A small
   window appears.
3. First run shows **Set up TransSped** — enter your **Trans Sped userID** (the
   email or phone registered for your cloud certificate) and click **Set up**.
   The app fetches your certificate and registers the PKCS#11 module into your
   normal Firefox profile.
4. The window then shows your status: **Installed in Firefox**, your account,
   and the certificate's expiry. From here you can **Update** (re-fetch),
   **Open ANAF login**, **Uninstall**, or view **About**.

Then use Firefox as usual (see [Daily use](#daily-use)).

To build the app from a checkout: `./scripts/build-app.sh` → `TransSped.app`.

### Uninstall

Open **TransSped**, then click **Uninstall** (with Firefox quit). It unregisters
the `TransSpedCloud` module from Firefox and deletes `~/.config/tscloud`
(including any remembered PIN). You can also unload it manually from Firefox →
Settings → Privacy & Security → **Security Devices** → **Unload**.

## Daily use

Open your normal Firefox, then:

1. Go to your ANAF SPV login (e.g. `https://pfinternet.anaf.ro`).
2. Choose the **certificate** authentication method.
3. Pick the **Trans Sped Cloud** certificate from the picker.
4. Enter your **PIN** and then the **OTP** in the native macOS dialogs (OTP
   from the Trans Sped app or email).
5. The SPV dashboard loads.

## How it works

- **One PIN + OTP dialog per login.** ANAF's F5 APM requests the client cert
  via TLS renegotiation, during which NSS never performs `C_Login` — so the
  module collects the secrets **itself, at signing time**, in a single native
  dialog with a PIN field, an OTP field, and a **"Remember PIN"** checkbox
  (mirroring the Windows KSP's one prompt). Tick "Remember PIN" and the PIN is
  saved to your macOS login **Keychain**, so later logins ask only for the OTP.
  A remembered PIN that stops working is forgotten automatically.
- **~1 OTP per login.** Your account has `SCAL = 2` — each signing
  authorization (SAD) is OTP-bound to one specific hash. A normal SPV login
  performs one client-cert handshake, so it costs one OTP, matching the
  Windows experience. See [docs/RUNBOOK.md](docs/RUNBOOK.md) for how to
  measure this on your first real login.
- **No TLS version pin needed.** The cloud only ever produces RSA PKCS#1 v1.5
  signatures (no RSA-PSS), so TLS 1.3 client auth would be impossible with
  this backend — but ANAF's certificate endpoints are **TLS 1.2-only**
  anyway, so Firefox always negotiates 1.2 with them and there is nothing to
  configure.
- **Firefox is required.** Chrome and Safari get client certificates from the
  macOS Keychain and have no PKCS#11 module loader; only Firefox (via NSS)
  can load `libtscloud-pkcs11.dylib`.
- **The module is inert for normal browsing.** NSS only invokes it when a
  site's `CertificateRequest` names a CA that matches your Trans Sped cert's
  issuer — in practice just ANAF.

## Build from source (developers)

### Prerequisites

- **Go 1.22+** (developed against 1.26; `go.mod` requires `go 1.22`)
- **Xcode Command Line Tools** (`xcode-select --install`) — provides `clang`/`cc`, needed for the cgo build
- `brew install opensc` — provides `pkcs11-tool` for testing/diagnostics (optional)
- **Firefox** — required at runtime; Chrome and Safari cannot use this module
- An existing **Trans Sped cloud qualified certificate**, already enrolled with ANAF (form 150) — enrollment itself is out of scope, see below

### Build

```bash
./scripts/build.sh       # libtscloud-pkcs11.dylib + tscloud-setup (CLI)
./scripts/build-app.sh   # TransSped.app (bundles the dylib + setup app)
```

Built binaries are not committed to git (see `.gitignore`); build them locally.

`tscloud-setup -user "<email or phone>"` is the standalone credential-fetch CLI
(the app does the same thing internally). It calls the Trans Sped CSC API
(`credentials/list` → `credentials/info`) and writes to `~/.config/tscloud/`:
`config.json` (baseURL, userID, credentialID, label), `leaf.der` (your cert),
and `intermediate*.der` (the issuing CA chain).

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
  "$(pwd)/libtscloud-pkcs11.dylib"`.
- **No `.h` file is generated by the build.** `-buildmode=c-shared` only
  emits a public header for `//export` directives living in `package main`;
  `pkcs11mod`'s `//export` glue lives in the vendored package, not in
  `cmd/pkcs11`. This is expected — the dylib still correctly exports
  `_C_GetFunctionList`/`_C_Initialize` (verify with `nm -gU
  libtscloud-pkcs11.dylib`), which is all a Cryptoki host needs.

## Security

- The **OTP** is single-use and collected per login via a native dialog —
  never stored or logged.
- The **signature PIN** is used only to authorize a signature. If you tick
  **"Remember PIN"**, it is stored in the macOS login **Keychain** (encrypted
  at rest, keyed to your credential ID under service `ro.transsped.macos`) via
  the `security` tool — never in a plaintext file. If you don't, it is
  discarded after the login. Uninstalling (via the app's **Uninstall** button) removes it. *(Caveat:
  the Keychain item is saved with `-A` so the module can read it without a
  per-process prompt, and the PIN is briefly visible in `security`'s process
  arguments while being saved — acceptable on a single-user Mac.)*
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
- **The OAuth portal** (`logincert.anaf.ro` → `login.anaf.ro`) requests
  `rsa_pkcs1_sha384`, which the cloud rejects; the working path is the F5 APM
  portal (`app.anaf.ro`) with SHA-256.

## Further reading

- [docs/RUNBOOK.md](docs/RUNBOOK.md) — step-by-step for your first real
  ANAF login, plus troubleshooting.
- [docs/specs/2026-07-14-anaf-macos-login-design.md](docs/specs/2026-07-14-anaf-macos-login-design.md)
  — the full design spec (architecture, CSC endpoint contracts, algorithm
  selection, risks).
