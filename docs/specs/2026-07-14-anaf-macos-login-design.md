# ANAF SPV login on macOS via a Trans Sped **cloud** qualified certificate — Design Spec

**Date:** 2026-07-14
**Status:** Approved for planning (Approach A, Firefox-login-only scope)
**Author:** reverse-engineering + design session

> **Update (2026-07-16) — shipped design differs on two points.** Real logins go
> through ANAF's **F5 BIG-IP APM** portal (`app.anaf.ro`), which requests the cert
> via TLS **renegotiation** and offers `rsa_pkcs1_sha256` — so there is **no
> TLS-1.2 pin and no dedicated Firefox profile**; the module registers into the
> normal profile and collects PIN+OTP at sign time. Delivery is the notarized
> `TransSped.app`. The CSC API findings below remain accurate. See the
> [README](../../README.md) and [RUNBOOK](../RUNBOOK.md) for the current design.

---

## 1. Goal & scope

Let the user log in to **ANAF SPV** (Spațiul Privat Virtual, `pfinternet.anaf.ro`) from **macOS** using their **Trans Sped cloud** qualified certificate — **without a Windows VM, without Wine, and without a physical token**.

**In scope:** interactive browser login in **Firefox** on macOS (Apple Silicon).
**Out of scope (non-goals):**
- Certificate **enrollment** with ANAF (form 150) — assumed already done on Windows.
- **Document signing** (PDF/PKCS#7) — that is EasySign's job; not needed for login.
- **Chrome/Safari** — they use the macOS Keychain, which has no hook for a cloud key. Firefox is required because it has its own PKCS#11 loader.
- **TLS 1.3** client auth — impossible with this cloud (see §7); we pin TLS 1.2.
- A headless OAuth **API-token** tool — explicitly deferred (user chose Firefox-login-only).

## 2. Background — what we are replicating

On Windows, EasySign / the **"Trans Sped Cloud KSP"** (a CNG Key Storage Provider) makes the cloud-held private key usable by the browser's TLS stack. When ANAF requests a client certificate mid-handshake, Windows SChannel calls the KSP's `NCryptSignHash`, which forwards the handshake hash to Trans Sped's cloud, prompts for an OTP, and returns the signature. macOS has **no CNG/KSP layer**, so we replace the KSP with a **PKCS#11 module** loaded into Firefox's NSS.

## 3. Validated findings (evidence base)

All of the following are **confirmed** — by decompiling the shipped .NET assemblies (`TS_CloudCrypto.dll`, `EasySign.exe`) and by a live end-to-end probe from this Mac (`ts_probe.py`, result: `PASS ✅ Signature Verified Successfully`).

- **Trans Sped cloud = a Cloud Signature Consortium (CSC) API** Remote Signing Service Provider. It exposes a **raw hash-signing** primitive (`signatures/signHash` signs a caller-supplied precomputed digest and returns raw RSA-PKCS#1 v1.5 bytes). This is exactly what TLS client auth needs.
- **Backend for standard qualified certs (this user):** `https://msign.transsped.ro/csc/v0/local/` — **reachable from macOS** over plain HTTPS, **no Authorization header** required for the calls we use (`credentials/list` returned HTTP 200 for a dummy user). The `/local/` in the path is *not* localhost-only.
  - (Mobile-eIDAS certs would use `https://services.cloudsignature.online/csc/v1/` with OAuth2 Bearer — that host timed out from here and is **not** this user's path.)
- **The user's credential is on the `msign` backend**, key is **RSA** (RSA-2048 expected for `TransSpedQCAG3`), and **`SCAL = 2`** (see §8 for UX impact).
- **Only RSA PKCS#1 v1.5** is produced (no PSS, no ECDSA). → TLS 1.2 only.
- **ANAF SPV login = client-certificate mutual TLS** to `https://logincert.anaf.ro/anaf-oauth2/v1/authorize`. The server supports TLS 1.2 **and** 1.3; we must force **1.2** (TLS 1.3 requires RSA-PSS for CertificateVerify, which the cloud cannot produce). Entry point: `pfinternet.anaf.ro` / `www.anaf.ro` → "Autentificare certificat".

### 3.1 CSC endpoint contracts (reverse-engineered, base = `https://msign.transsped.ro/csc/v0/local/`)

| Endpoint | Request body | Response |
|---|---|---|
| `credentials/list` | `{"userID":"<email or phone>"}` | `{"credentialIDs":[...], "nextPageToken":...}` |
| `credentials/info` | `{"credentialID":"...","certInfo":"true","certificates":"chain"}` | `{cert/certificates[], key{algo,len,status}, authMode, SCAL, multisign, otp, PIN, description, lang}` |
| `credentials/sendOTP` | `{"credentialID":"..."}` | triggers OTP to the Trans Sped OTP app / SMS |
| `credentials/authorize` | `{"credentialID":"...","numSignatures":"1","hash":["<b64>"],"PIN":"<sig pwd>","OTP":"<otp>"}` | `{"SAD":"...","expiresIn":...}` |
| `signatures/signHash` | `{"credentialID":"...","signAlgo":"<OID>","hashAlgo":"<OID>","signAlgoParams":"","SAD":"...","hash":["<b64>"]}` | `{"signatures":["<b64 RSA sig>"]}` |
| `credentials/extendTransaction` | `{"credentialID":"...","SAD":"..."}` | refreshed SAD |

### 3.2 Algorithm selection (signAlgo / hashAlgo by digest length)

| Hash len | signAlgo OID | hashAlgo OID | Meaning |
|---|---|---|---|
| 20 | `1.3.14.3.2.29` | `1.3.14.3.2.26` | SHA-1 withRSA |
| 32 | `1.2.840.113549.1.1.11` | `2.16.840.1.101.3.4.2.1` | **SHA-256 withRSA ← TLS 1.2 rsa_pkcs1_sha256 (primary path, probe-proven)** |
| 48 | `1.2.840.113549.1.1.12` | `2.16.840.1.101.3.4.2.2` | SHA-384 withRSA |
| 64 | `1.2.840.113549.1.1.13` | `2.16.840.1.101.3.4.2.3` | SHA-512 withRSA |
| 36 | `1.2.840.113549.1.1.1` | `1.3.6.1.4.1.2706.2.4.1.1` | raw rsaEncryption (legacy TLS1.0/1.1 MD5+SHA1) |

## 4. Architecture (Approach A)

```
 Firefox (dedicated "ANAF" profile, TLS pinned to 1.2)
   │  ANAF requests client cert → NSS needs a CertificateVerify signature
   ▼
 NSS ──C_Sign(DigestInfo(SHA-256, handshakeHash))──▶  libtscloud-pkcs11.dylib
                                                         │ 1. parse DigestInfo → (hashAlg, rawHash)
                                                         │ 2. native OTP dialog + sendOTP
                                                         │ 3. authorize(PIN,OTP,rawHash) → SAD
                                                         │ 4. signHash(SAD, rawHash, sha256WithRSA)
                                                         ▼
                                                   Trans Sped cloud HSM  (msign CSC v0)
                                                         │ RSA PKCS#1 v1.5 signature
   ◀────────────────────── signature returned up the stack ─┘
   │  TLS handshake completes
   ▼
 logincert.anaf.ro  →  OAuth code  →  pfinternet.anaf.ro SPV session ✅
```

## 5. Components

### 5.1 CSC client library (Go package `cscclient`)
Pure Go, no cgo. Functions mirroring §3.1: `List(userID)`, `Info(credID)`, `SendOTP(credID)`, `Authorize(credID, pin, otp, hashB64, numSigs)`, `SignHash(credID, sad, hashB64, signAlgo, hashAlgo)`. Configurable base URL (default `msign` v0), proxy-aware, 30 s timeout. Returns typed structs. **Reuses exactly the request/response shapes the probe already proved.**

### 5.2 PKCS#11 module (`libtscloud-pkcs11.dylib`)
Built with **Namecoin `pkcs11mod`** (Go → C-shared), which already vends certificates to Firefox/NSS in production, so we implement a Go interface rather than raw Cryptoki C.

**Objects vended** (one slot, one token, always "present"):
- **Certificate object** — `CKO_CERTIFICATE`, `CKC_X_509`, `CKA_VALUE` = leaf DER (from `credentials/info`), `CKA_ID` = key id (SHA-1 of the modulus), plus `CKA_SUBJECT`/`CKA_ISSUER`/`CKA_SERIAL_NUMBER`/`CKA_LABEL`, `CKA_TOKEN`=true.
- **Private-key object** — `CKO_PRIVATE_KEY`, `CKK_RSA`, `CKA_ID` = **same key id** (this linkage is how NSS pairs cert↔key), `CKA_MODULUS`/`CKA_PUBLIC_EXPONENT` (from the leaf cert), `CKA_SIGN`=true, `CKA_TOKEN`/`CKA_PRIVATE`/`CKA_SENSITIVE`=true. No key material — signing is delegated.
- **Intermediate cert object(s)** — `CKO_CERTIFICATE` for the Trans Sped intermediate(s) (`TransSpedQCAG3`), so NSS can build and present the full chain. (Bundled `.crt` files are available as a fallback source.)

**Mechanisms advertised:** `CKM_RSA_PKCS` (raw PKCS#1 v1.5 — what NSS uses for TLS 1.2 client auth).

**Key call — `C_Sign`:**
1. NSS calls `C_SignInit(CKM_RSA_PKCS, privKey)` then `C_Sign(data)`. For `CKM_RSA_PKCS`, `data` is the **DER `DigestInfo`** (≈51 bytes for SHA-256).
2. Parse the `DigestInfo` → recover the hash OID and the raw digest. Map hash OID/length → `(signAlgo, hashAlgo)` via §3.2. (Primary/proven path: SHA-256 → `sha256WithRSA` + 32-byte digest.)
3. Run the CSC flow: `SendOTP` → **native OTP+PIN dialog** → `Authorize(...,numSignatures:"1")` → `SignHash(...)`.
4. Return the raw signature bytes (256 for RSA-2048).
- *Fallback if the server rejects the parsed-hash path:* call `signHash` with `signAlgo = rsaEncryption (1.1.1)` passing the **full DigestInfo** bytes (server does raw PKCS#1 v1.5 padding only). Selectable by config flag.

**Sessions/PIN:** collect the Trans Sped **signature PIN/password** once via Firefox's PKCS#11 PIN dialog at `C_Login` and cache it for the session; collect the **OTP** per signature at `C_Sign` (it is dynamic). Optionally set `CKA_ALWAYS_AUTHENTICATE` to get a pre-sign hook — evaluated in Phase 2.

### 5.3 OTP / PIN user interaction
At `C_Sign` the module must prompt the user for the OTP (and PIN if not cached). Implementation: shell out to **`osascript`** (`display dialog … with hidden answer`) — zero extra dependencies, native modal. The module first calls `sendOTP`, then shows the dialog telling the user to enter the code from their **Trans Sped OTP app** (iOS `id1507000103` / Android `at.tugraz.iaik.signapp`) or SMS. A small AppKit helper is a later polish option; `osascript` is the MVP.

### 5.4 Firefox "ANAF" profile
A **dedicated profile** (so global TLS settings don't affect normal browsing):
- Load the module: `modutil -dbdir sql:<profile> -add "TransSpedCloud" -libfile <dylib>` (or the Security Devices UI).
- `about:config`: `security.tls.version.min = 3` and `security.tls.version.max = 3` → **TLS 1.2 only**.
- Ensure the Trans Sped **intermediate CA** is present (imported or vended by the module) so the client-cert chain builds. ANAF already trusts the Trans Sped CA for the enrolled cert.
- Launch: `/Applications/Firefox.app/Contents/MacOS/firefox -P anaf -no-remote`.

## 6. Data flow (login sequence)
1. User launches the ANAF Firefox profile → `pfinternet.anaf.ro` → "Autentificare certificat".
2. Browser redirects to `logincert.anaf.ro/anaf-oauth2/v1/authorize`; server sends `CertificateRequest`.
3. NSS selects our token's cert (user confirms in the cert picker), computes the handshake transcript **SHA-256** hash, wraps it as `DigestInfo`, and calls our `C_Sign`.
4. Module: `sendOTP` → OTP dialog → `authorize(PIN,OTP,hash)` → `signHash` → returns signature.
5. NSS finishes the handshake; ANAF completes OAuth; the SPV session opens in the browser.

## 7. Why TLS 1.2 (hard constraint)
The cloud emits **only** `RSASSA-PKCS1-v1_5` (`signAlgoParams` always empty; no PSS OID anywhere). **TLS 1.3 mandates RSA-PSS** for CertificateVerify → impossible. **TLS 1.2** CertificateVerify with `rsa_pkcs1_sha256` uses PKCS#1 v1.5 over a 32-byte SHA-256 hash → exactly the proven path. Hence the profile pins `max = 1.2`. `logincert.anaf.ro` supports TLS 1.2 (verified).

## 8. UX reality — `SCAL = 2`
`SCAL=2` means each SAD is **OTP-authorized and bound to one specific hash**, so **every distinct CertificateVerify signature costs one OTP**. A browser SPV login normally performs **one** client-cert handshake to `logincert.anaf.ro` (HTTP keep-alive / a single connection), so the expected cost is **~1 OTP per login** — identical to the current Windows experience. Phase 3 will **measure** the real count; if it is >1, mitigations are limited by SCAL2 (hashes aren't known in advance), and the escalation path is the deferred API-token tool (1 OTP per ~90 days). The mid-handshake OTP wait is proven tolerable because the Windows KSP does the same thing against the same ANAF server.

## 9. Error handling & edge cases
- Cloud/network error, wrong PIN/OTP, expired SAD → return `CKR_FUNCTION_FAILED`/`CKR_PIN_INCORRECT`; surface a clear dialog; let the user retry the login.
- Handshake timeout while waiting for OTP → user must approve promptly; dialog states this. Configurable client timeout.
- Wrong backend (empty `credentials/list`) → module logs a clear message pointing to the v1 backend (out of scope for this user).
- Multiple credentials → module config selects by `credentialID` or cert serial (config has `1c7d9e57092a35aba42e913c`).

## 10. Testing strategy
- **Phase 1:** unit tests for `cscclient` against the real credential (the probe already returns `PASS`); assert a valid RSA sig over a random 32-byte hash verifies against the leaf cert.
- **Phase 2:** `pkcs11-tool --module ./libtscloud-pkcs11.dylib -O` (objects list) and `--sign` (sign a hash) for Cryptoki-level sanity; then a **local Go TLS server** requiring client auth, driven by a PKCS#11-aware client, to prove a full mutual-TLS handshake uses the module end-to-end **before** touching ANAF.
- **Phase 3:** real login to `pfinternet.anaf.ro` in the ANAF Firefox profile; measure OTP count; if needed capture with `SSLKEYLOGFILE` + Wireshark to confirm the negotiated version and signature scheme.

## 11. Risks & mitigations
| Risk | Likelihood | Mitigation |
|---|---|---|
| NSS attribute-compat bugs in `C_GetAttributeValue`/`FindObjects` (top dev risk) | Med | Use `pkcs11mod` (proven with NSS); iterate with `pkcs11-tool` + NSS logging (`NSS_DEBUG_PKCS11_MODULE`) |
| TLS 1.3 negotiated by mistake | Low | Pin `security.tls.version.max=3` in the profile; verify with a handshake capture |
| >1 OTP per login (SCAL2) | Low–Med | Measure in Phase 3; fall back to API-token tool if painful |
| Handshake times out during OTP entry | Low | Instant push OTP; dialog urges promptness; mirrors working Windows flow |
| `.dylib` arch/codesign rejected by Firefox | Low | Build **arm64** (matches Firefox on this Mac); `codesign -s -` ad-hoc; clear quarantine xattr |
| `msign v0` is undocumented / could change | Low | It is exactly what shipped EasySign depends on; pin behavior with tests |

## 12. Build & packaging
- **Toolchain:** Go (cgo on) + `pkcs11mod`; `pkcs11-tool` (OpenSC, already present) and `modutil`/`certutil` (`brew install nss`) for testing.
- **Build:** `CGO_ENABLED=1 GOARCH=arm64 go build -buildmode=c-shared -o libtscloud-pkcs11.dylib ./cmd/pkcs11`.
- **Sign:** `codesign -s - libtscloud-pkcs11.dylib` (ad-hoc); `xattr -dr com.apple.quarantine` if needed.
- **Deliverables:** the `.dylib`, a `setup.sh` that creates the Firefox profile + loads the module + pins TLS 1.2, and a short README.

## 13. Phased implementation plan (high-level)
- **Phase 0 — Validate** ✅ (done: `ts_probe.py` → PASS, SCAL=2).
- **Phase 1 — `cscclient` Go library** + tests against the real cert; port the probe's proven calls.
- **Phase 2 — PKCS#11 module** (pkcs11mod): objects/attributes, `C_Sign`→CSC, `osascript` OTP dialog; validate with `pkcs11-tool` + a local mTLS server.
- **Phase 3 — Firefox profile + real ANAF login**; measure OTP count; fix any NSS/handshake issues.
- **Phase 4 — Packaging**: `setup.sh`, README, ad-hoc codesign.

## 14. References / prior art
- `dumitrucatalin/Chromium-Android-CSC-TLS-MUTUAL-AUTH` — forwards a TLS CertificateVerify to a **Trans Sped** CSC `signHash` endpoint (direct precedent).
- Namecoin **`pkcs11mod`** — Go framework that vends certs to Firefox/NSS via PKCS#11 (implementation scaffold).
- certSIGN **Paperless vToken** (macOS) — ships the same architecture for a different QTSP (reference).
- `silviancretu.ro` — the community Firefox + PKCS#11 ANAF-on-macOS/Linux flow we extend.
- CSC (Cloud Signature Consortium) API v0/v1 spec — authoritative endpoint schemas.
- Local: `../ts_probe.py` (the validated probe), decompiled `TS_CloudCrypto.dll` findings.
