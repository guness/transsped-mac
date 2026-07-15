# Runbook: your first real ANAF SPV login

This is the step-by-step for going from a clean checkout to a working ANAF
SPV login in Firefox, using your real Trans Sped cloud certificate. It
covers the same ground as the [README](../README.md) but in the exact
order to run things, with what to expect at each step, plus troubleshooting
for when something doesn't go as expected.

The live ANAF login itself (steps 4-5 below) is a step only you can do — it
needs your real Trans Sped account and a live OTP.

## Step 1 — Build

```bash
cd /path/to/EasySign-macos
./scripts/build.sh
```

Expect:
```
built: libtscloud-pkcs11.dylib, tscloud-setup
```
(A benign `ld: warning: ignoring duplicate libraries: '-lpkcs11_exported'`
may appear before that line — this is expected, from the vendored
`pkcs11mod` C shim being linked twice; it is not an error.)

## Step 2 — Fetch your cloud certificate

```bash
./tscloud-setup -user "<your Trans Sped email or phone>"
```

Expect output like:
```
credentialID: <your credential id>
Saved config + 2 cert(s) to /Users/<you>/.config/tscloud (SCAL=2)
```

This writes `~/.config/tscloud/config.json`, `leaf.der`, and
`intermediate*.der`. If you see `no credentials for that user on
https://msign.transsped.ro/csc/v0/local/` — see Troubleshooting below
("empty credential list").

## Step 3 — Set up the dedicated Firefox profile

```bash
./scripts/setup-firefox.sh
```

Expect four numbered steps to print, ending with:
```
Profile ready. Launch:  /Applications/Firefox.app/Contents/MacOS/firefox -profile /Users/<you>/.tscloud-firefox -no-remote
```

This must run **after** Step 2 — it imports intermediate certificates from
`~/.config/tscloud`, which only exist once `tscloud-setup` has run. Run it
again any time you rebuild the dylib (it's idempotent: `-force` on the
module add, and the TLS pin/imports are skipped if already present).

## Step 4 — Launch and log in

```bash
/Applications/Firefox.app/Contents/MacOS/firefox -profile "$HOME/.tscloud-firefox" -no-remote
```

1. Navigate to `https://pfinternet.anaf.ro`.
2. Click **"Autentificare certificat"**.
3. Firefox shows a certificate picker — select the **Trans Sped Cloud**
   certificate.
4. Firefox may first ask for the token's **PIN** (this is your Trans Sped
   signature PIN/password, not your ANAF password) — enter it.
5. A native macOS dialog appears: **"ANAF login — Trans Sped OTP"**. Check
   your Trans Sped OTP app or SMS for the code, enter it, click OK.
6. Expect the SPV dashboard to load within a few seconds of approving the
   OTP.

## Step 5 — Measure the OTP count

Record, right after your first successful login: **how many separate OTP
dialogs appeared during one full login** (from clicking "Autentificare
certificat" to the SPV dashboard loading).

Expected: **1**. The account's `SCAL = 2` means every distinct
CertificateVerify signature costs one OTP, and a normal browser SPV login
performs exactly one client-cert TLS handshake to `logincert.anaf.ro`
(HTTP keep-alive keeps the rest of the session on that one connection), so
one OTP is the expected steady state — matching the existing Windows
experience.

If you saw **more than 1**, note here where the extra prompt(s) occurred
(e.g. a redirect that re-negotiated TLS, a second host requesting a client
cert) — that's useful data for follow-up work, since SCAL=2 means the
mitigation options are limited (hashes aren't known ahead of time to
pre-authorize).

If the handshake **timed out** while you were entering the OTP, note the
approximate delay before it gave up.

## Troubleshooting

| Symptom | Check |
|---|---|
| Trans Sped Cloud certificate isn't offered in the picker | `about:config` in the `tscloud-firefox` profile → confirm `security.tls.version.max` == `3` (TLS 1.2). Also confirm the module still lists the cert: `pkcs11-tool --module "<absolute path>/libtscloud-pkcs11.dylib" --list-objects` (must be an **absolute** path — a relative one fails with "relative path not allowed in hardened program"). |
| Handshake fails or times out | Approve the OTP dialog promptly — the TLS handshake is waiting on it. To confirm what was actually negotiated, capture the session: `SSLKEYLOGFILE=/tmp/ff.keys /Applications/Firefox.app/Contents/MacOS/firefox -profile "$HOME/.tscloud-firefox" -no-remote`, reproduce the login, then open the capture in Wireshark (set `tls.keylog_file` to `/tmp/ff.keys` in Preferences → Protocols → TLS) and confirm the handshake used **TLS 1.2** and signature scheme **rsa_pkcs1_sha256**. |
| Module not listed in Firefox at all | Firefox → Settings → Privacy & Security → **Security Devices** (bottom of the page). Confirm "TransSpedCloud" is listed and loaded. If missing, re-run `./scripts/setup-firefox.sh` — it will report if the dylib or `modutil`/`certutil` couldn't be found. |
| `tscloud-setup` reports "no credentials for that user" / empty list | Your certificate may be a **mobile-eIDAS** credential rather than a standard qualified cert — those live on a different backend (`https://services.cloudsignature.online/csc/v1/` with OAuth2), which this tool does not support (it targets `https://msign.transsped.ro/csc/v0/local/` only). Confirm with whoever issued your Trans Sped credential which backend it's on. |
| PIN rejected / OTP rejected | Confirm you're entering your **Trans Sped signature PIN/password** (not your ANAF portal password) at the Firefox PIN prompt, and that the OTP is the current one from the Trans Sped OTP app or SMS (they expire quickly — request a fresh login attempt if it lapses). |

## After a successful login

Nothing further to do — the `tscloud-firefox` profile keeps the module
loaded and the TLS pin persists across launches. Re-run `./scripts/build.sh`
+ `./scripts/setup-firefox.sh` only if you rebuild the dylib (e.g. after a
code change) or need to reload the module.

---

## ✅ CONFIRMED WORKING FLOW (2026-07-15)

Verified end-to-end: a Trans Sped **cloud** qualified cert logging into ANAF SPV
on macOS Firefox, reproducibly.

**How the login actually works:**
1. Launch the dedicated profile:
   `/Applications/Firefox.app/Contents/MacOS/firefox -profile "$HOME/.tscloud-firefox" -no-remote`
2. Start the ANAF login and choose the **certificate** method. Depending on the
   entry point this lands on either the OAuth flow (`logincert.anaf.ro`) or the
   **F5 BIG-IP APM** flow (`app.anaf.ro/my.policy`). The F5 APM certificate
   option is the confirmed-working path.
3. Pick the **Trans Sped Cloud** cert. Our module then pops a **PIN dialog**
   (signature password) and an **OTP dialog** — enter both (OTP from the Trans
   Sped app or email). This happens **once per client-cert TLS handshake**
   (SCAL=2), so a full login may prompt more than once.
4. The F5 APM policy completes and the SPV dashboard loads.

**Why the module is configured "not login required":** ANAF's F5 APM requests
the cert via TLS **renegotiation**, during which NSS never performs a
`C_Login`/PIN prompt. A login-required token therefore can't expose its key and
NSS sends an empty certificate (the original infinite-OTP loop). Reporting the
token as not-login-required + `CKA_PRIVATE=false` lets NSS present the cert
during renegotiation; the module collects PIN+OTP itself at sign time.

**Known trade-offs / follow-ups:**
- Because the token is not login-required, NSS may offer the cert on more ANAF
  connections, adding cloud round-trips (slower browsing). Tunable later.
- The OAuth portal (`login.anaf.ro`) may negotiate SHA-384, which the msign
  cloud cannot sign; the F5 APM portal (SHA-256) is the working path.
- Debug: `touch ~/.config/tscloud/DEBUG` → logs to `~/.config/tscloud/pkcs11-debug.log`
  (visible for `pkcs11-tool`/CLI; Firefox's sandbox blocks it).
