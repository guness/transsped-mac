# C_Sign end-to-end proof (local CSC mock)

Proves `libtscloud-pkcs11.dylib`'s `C_Sign` works through the real C ABI,
headlessly, against a **local mock CSC server** — no real Trans Sped cloud
account, no live OTP dialog. This is the sign-path proof; the full TLS
handshake through NSS/Firefox is validated separately by the real ANAF
login, which needs the user (see `scripts/mtls_test.md` if/when that
happens — not part of this task).

Chain proven end to end:

```
pkcs11-tool --sign
  -> dlopen(libtscloud-pkcs11.dylib)
  -> C_SignInit / C_Sign         (real Cryptoki C ABI)
  -> token.Backend.Sign
  -> csc.Signer.SignDigestInfo   (sendOTP -> authorize -> signHash)
  -> HTTP POST to test/cscmock
  -> RSA-PKCS1v15/SHA-256 signature
  -> back through the C ABI into sig.bin
  -> openssl verifies sig.bin against the self-signed leaf cert's pubkey
```

## What changed to make this possible

1. **`cmd/pkcs11/main.go`** — headless OTP override. If `TSCLOUD_OTP` is set
   in the environment, the module uses `otp.Static{Value: ...}` instead of
   popping the interactive `osascript` dialog (`otp.OSAScript{}`). Unset
   (the default, real-user path) is unchanged — this only affects
   automated/CI runs that explicitly opt in.
2. **`test/cscmock/main.go`** — a standalone throwaway CSC server. Given an
   RSA private key (`-key`), it implements just enough of the CSC API for
   `csc.Client`/`csc.Signer` to complete a full sign flow:
   - `POST /credentials/sendOTP` -> `{}`
   - `POST /credentials/authorize` -> `{"SAD":"mock-sad"}`
   - `POST /signatures/signHash` -> decodes the base64 digest from the
     request, signs it with `rsa.SignPKCS1v15(..., crypto.SHA256, digest)`,
     and returns `{"signatures":["<base64 sig>"]}` — exactly what the real
     cloud returns for `signAlgo=sha256WithRSA` over a 32-byte hash.
3. **`scripts/sign-test.sh`** — orchestrates a full run: builds the dylib,
   generates a throwaway RSA-2048 self-signed cert, starts the mock server,
   drives `pkcs11-tool --sign` through the compiled module, and verifies the
   resulting signature against the cert's public key with `openssl pkeyutl`.

## Prerequisites

- `./scripts/build.sh` has been run (the test script runs it again anyway).
- OpenSC's `pkcs11-tool` is installed (`brew install opensc`).
- `openssl` and `go` are on `PATH`.

## Procedure

```bash
./scripts/sign-test.sh
```

The script:

1. Builds `libtscloud-pkcs11.dylib` via `./scripts/build.sh`.
2. Creates a throwaway `$WORK` dir; generates an RSA-2048 key + self-signed
   cert (`key.pem` / `leaf.der`) via
   `openssl req -x509 -newkey rsa:2048 -nodes ...`.
3. Writes `$WORK/config.json` pointing `baseURL` at
   `http://127.0.0.1:8099/`.
4. Starts `go run ./test/cscmock -key "$WORK/key.pem" -addr 127.0.0.1:8099`
   in the background, polls (up to ~5s) until it accepts connections, and
   traps EXIT to kill it and `rm -rf "$WORK"`.
5. Writes 32 random bytes to `$WORK/digest.bin` (stand-in for a SHA-256
   digest — `csc.ParseHashInput` treats any bare 32-byte input as a SHA-256
   digest and selects `signAlgo=1.2.840.113549.1.1.11` /
   `hashAlgo=2.16.840.1.101.3.4.2.1`, i.e. sha256WithRSA / SHA-256).
6. Runs, with `TSCLOUD_OTP=000000` (headless OTP) and
   `TSCLOUD_DIR="$WORK"`:
   ```
   pkcs11-tool --module "$(pwd)/libtscloud-pkcs11.dylib" \
     --login --pin 1234 --sign --mechanism RSA-PKCS \
     --input-file "$WORK/digest.bin" --output-file "$WORK/sig.bin"
   ```
7. Verifies: `openssl x509 -inform der -in "$WORK/leaf.der" -pubkey -noout`
   then
   `openssl pkeyutl -verify -pubin -inkey pub.pem -sigfile sig.bin -in digest.bin -pkeyopt rsa_padding_mode:pkcs1 -pkeyopt digest:sha256`.
   Expects `Signature Verified Successfully`.

If it fails, re-run with `P11MOD_TRACE=1` prefixed to the `pkcs11-tool`
invocation (or export it before calling `./scripts/sign-test.sh`) to see the
individual Cryptoki calls pkcs11mod is receiving/returning.

## Actual run — 2026-07-14

```
$ ./scripts/sign-test.sh
==> [1/6] Building dylib (./scripts/build.sh)
# tscloud/cmd/pkcs11
ld: warning: ignoring duplicate libraries: '-lpkcs11_exported'
built: libtscloud-pkcs11.dylib, tscloud-setup
==> [2/6] Generating throwaway RSA-2048 key + self-signed cert in /var/folders/yr/3lvr1kzs0k9_bblrp3blbpdh0000gn/T/tmp.eiGqm9O8Zk
.....+++++++++++++++++++++++++++++++++++++++*...
-----
==> [3/6] Writing /var/folders/yr/.../tmp.eiGqm9O8Zk/config.json (leaf.der already in place)
==> [4/6] Starting mock CSC server on 127.0.0.1:8099
2026/07/14 18:42:27 cscmock: listening on 127.0.0.1:8099 (key=/var/folders/yr/.../tmp.eiGqm9O8Zk/key.pem)
    mock CSC server is up (pid 71190)
==> [5/6] Signing a random 32-byte digest through the compiled module
Using slot 0 with a present token (0x0)
Using signature algorithm RSA-PKCS
==> [6/6] Verifying the signature against the cert's public key
Signature Verified Successfully
PASS: C_Sign end-to-end signature verified against the leaf certificate.
```

**Result: PASS**, first attempt — no `P11MOD_TRACE` needed. Confirmed after
the run that the trap correctly killed the mock server and freed the port
(`lsof -i :8099` empty, no `cscmock` process left in `ps aux`) and removed
the temp `$WORK` dir.

This proves the entire chain end to end: `pkcs11-tool` successfully
`dlopen()`s the dylib, drives `C_Login` / `C_SignInit` / `C_Sign` over the
real Cryptoki C ABI, `Backend.Sign` forwards to `csc.Signer.SignDigestInfo`,
which runs the full `sendOTP -> authorize -> signHash` flow over HTTP
against `test/cscmock`, and the RSA-PKCS1v15/SHA-256 signature that comes
back through the C ABI verifies against the self-signed leaf certificate's
public key.

## Housekeeping

After this change: `go build ./...`, `go test ./...`, and `go vet ./...`
all pass clean; `gofmt -l` reports no issues for any non-vendor `.go` file
(`cmd/pkcs11/main.go` and `test/cscmock/main.go` are clean — the two files
`gofmt -l` does flag, under `vendor/github.com/miekg/pkcs11/`, are
pre-existing vendored third-party code untouched by this task).

No real credentials, live OTP, or network access to the actual Trans Sped
cloud are involved anywhere in this test — the key, cert, and CSC server
are all generated/run locally and thrown away (`trap ... EXIT` cleanup).
