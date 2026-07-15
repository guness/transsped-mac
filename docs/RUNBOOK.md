# Runbook: your first real ANAF SPV login

This is the step-by-step for going from the packaged app to a working ANAF
SPV login in your **normal** Firefox, using your real Trans Sped cloud
certificate. It covers the same ground as the [README](../README.md) but in
the exact order to run things, with what to expect at each step, plus
troubleshooting.

The live ANAF login itself (Step 2 below) is a step only you can do — it
needs your real Trans Sped account and a live OTP.

## Step 1 — Install with the app

1. **Quit Firefox** — a security module can't be added while it's running.
2. Open **`TransSped.app`** (double-click, or `open "TransSped.app"`).
3. Enter your **Trans Sped userID** (the email or phone registered for your
   cloud certificate) when the dialog asks.
4. Expect **"Setup complete."** Behind the scenes the app fetched your
   certificate to `~/.config/tscloud/`, copied the module there, and appended
   a `TransSpedCloud` entry to your default profile's `pkcs11.txt`.

(Building the app from a checkout: `./scripts/build-app.sh`. The standalone
credential-fetch CLI still exists — `./scripts/build.sh` then
`./tscloud-setup -user "<email or phone>"` — but the app does all of this.)

## Step 2 — Log in

Open your **normal** Firefox, then:

1. Navigate to your ANAF SPV login (e.g. `https://pfinternet.anaf.ro`).
2. Choose the **certificate** authentication method. Depending on the entry
   point this lands on either the OAuth flow (`logincert.anaf.ro`) or the
   **F5 BIG-IP APM** flow (`app.anaf.ro/my.policy`). **The F5 APM certificate
   option is the confirmed-working path.**
3. Firefox shows a certificate picker — select the **Trans Sped Cloud**
   certificate.
4. The module pops a **PIN dialog** (your Trans Sped signature PIN/password,
   not your ANAF portal password) and then an **OTP dialog** — enter both (OTP
   from the Trans Sped app or email).
5. Expect the SPV dashboard to load within a few seconds of approving the OTP.

## Step 3 — Measure the OTP count

Right after your first successful login, note **how many separate PIN/OTP
dialogs appeared** during one full login (from choosing the certificate
method to the SPV dashboard loading).

Expected: **1** — the account's `SCAL = 2` means every distinct
CertificateVerify signature costs one OTP, and a normal login performs one
client-cert handshake. The F5 APM requests the cert via TLS **renegotiation**,
so in some flows you may see the prompt more than once; if so, note where the
extra prompt(s) occurred. If the handshake **timed out** while you were
entering the OTP, note the approximate delay before it gave up.

## Troubleshooting

| Symptom | Check |
|---|---|
| Trans Sped Cloud certificate isn't offered in the picker | Confirm the module is loaded (Firefox → Settings → Privacy & Security → **Security Devices** → "TransSpedCloud"). From a terminal, confirm the module still vends the cert: `pkcs11-tool --module "$HOME/.config/tscloud/libtscloud-pkcs11.dylib" --list-objects` (must be an **absolute** path — a relative one fails with "relative path not allowed in hardened program"). |
| Login loops / repeated OTP prompts with no progress | This was the original bug when the token was login-required (NSS sent an empty cert during F5 renegotiation). The shipped module reports **not login-required** to avoid it; if you see it, you're likely running an old dylib — re-run the app to refresh `~/.config/tscloud/libtscloud-pkcs11.dylib`. |
| Handshake fails or times out | Approve the PIN/OTP dialogs promptly — the TLS handshake is waiting on them. To confirm what was negotiated, capture the session: `SSLKEYLOGFILE=/tmp/ff.keys open -a Firefox`, reproduce the login, then open the capture in Wireshark (set `tls.keylog_file` to `/tmp/ff.keys` under Preferences → Protocols → TLS) and confirm the handshake used **TLS 1.2** and signature scheme **rsa_pkcs1_sha256**. |
| Module not listed in Firefox at all | Confirm `pkcs11.txt` in your default profile contains a `name=TransSpedCloud` line. Re-run the app (with Firefox quit) to re-register. To find your default profile: it's the `[Install…] Default` (or `Default=1`) entry in `~/Library/Application Support/Firefox/profiles.ini`. |
| `TransSped.app` reports "no cloud credential found" | Your certificate may be a **mobile-eIDAS** credential rather than a standard qualified cert — those live on a different backend (`https://services.cloudsignature.online/csc/v1/` with OAuth2), which this tool does not support (it targets `https://msign.transsped.ro/csc/v0/local/` only). Confirm with whoever issued your Trans Sped credential which backend it's on. |
| PIN rejected / OTP rejected | Confirm you're entering your **Trans Sped signature PIN/password** (not your ANAF portal password), and that the OTP is the current one from the Trans Sped app or SMS (they expire quickly — start a fresh login attempt if it lapses). |

## Uninstall / reset

```bash
open "TransSped.app" --args -uninstall
```

Unregisters `TransSpedCloud` from your Firefox profile and deletes
`~/.config/tscloud`. (Or unload it manually: Firefox → Settings → Privacy &
Security → **Security Devices** → **Unload**.) To reinstall, just run the app
again.

---

## ✅ CONFIRMED WORKING FLOW (2026-07-15)

Verified end-to-end: a Trans Sped **cloud** qualified cert logging into ANAF SPV
on macOS **normal** Firefox, reproducibly, via `TransSped.app`.

**Why the module is configured "not login required":** ANAF's F5 APM requests
the cert via TLS **renegotiation**, during which NSS never performs a
`C_Login`/PIN prompt. A login-required token therefore can't expose its key and
NSS sends an empty certificate (the original infinite-OTP loop). Reporting the
token as not-login-required + `CKA_PRIVATE=false` lets NSS present the cert
during renegotiation; the module collects PIN+OTP itself at sign time.

**Known trade-offs / follow-ups:**
- Because the token is not login-required, NSS may offer the cert on more ANAF
  connections, adding cloud round-trips. In practice the module stays inert for
  non-ANAF browsing (NSS only invokes it when a site's CA list matches the
  Trans Sped issuer).
- The OAuth portal (`login.anaf.ro`) may negotiate SHA-384, which the msign
  cloud cannot sign; the F5 APM portal (SHA-256) is the working path.
- Debug: `touch ~/.config/tscloud/DEBUG` → logs to
  `~/.config/tscloud/pkcs11-debug.log` (visible for `pkcs11-tool`/CLI hosts;
  Firefox's sandbox blocks it, so capture the TLS session instead).
