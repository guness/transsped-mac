#!/usr/bin/env python3
"""
ts_probe.py -- Validation probe for using a Trans Sped CLOUD qualified certificate
from macOS, as the first step toward logging in to ANAF SPV WITHOUT Windows.

It proves the whole premise end-to-end, in order:

  STAGE 1  credentials/list   -> your account is reachable on the cloud API, and
                                 which credential(s) it holds.
  STAGE 2  credentials/info   -> reads your certificate + its signing constraints
                                 (SCAL mode, key type/size, cert chain). This is
                                 everything needed to PRESENT the cert in a TLS
                                 handshake. Saves leaf_cert.pem / chain.pem.
  STAGE 3  sendOTP+authorize+signHash (opt-in) -> THE LINCHPIN: the cloud signs an
                                 ARBITRARY 32-byte hash and the signature verifies
                                 against your cert's public key. That is exactly the
                                 operation a TLS client-certificate login performs.

It talks ONLY to Trans Sped's official CSC endpoint over HTTPS -- the same API the
Windows "EasySign" app uses. Your PIN and OTP are entered interactively (getpass),
are NEVER written to disk or printed, and are sent only to the Trans Sped endpoint.
This script does NOT log you into ANAF; it validates the cryptographic building block.

Run:  python3 ts_probe.py
"""

import os
import sys
import json
import base64
import getpass
import subprocess
import urllib.request
import urllib.error

# ---- Backends (from decompiling TS_CloudCrypto.dll) ---------------------------
# Standard qualified certs (Trans Sped QCA G3)  -> msign v0  (reachable from macOS)
# Mobile eIDAS certs (Trans Sped Mobile eIDAS)  -> cloudsignature.online v1 (OAuth2)
MSIGN_V0 = "https://msign.transsped.ro/csc/v0/local/"
CSC_V1   = "https://services.cloudsignature.online/csc/v1/"
BASE_URL = MSIGN_V0  # default; override with:  BASE=... python3 ts_probe.py
if os.environ.get("BASE"):
    BASE_URL = os.environ["BASE"].rstrip("/") + "/"

# signAlgo/hashAlgo OID pairs, selected by hash length (mirrors the DLL logic).
OID_BY_HASHLEN = {
    20: ("1.3.14.3.2.29",            "1.3.14.3.2.26"),          # SHA-1  withRSA
    32: ("1.2.840.113549.1.1.11",    "2.16.840.1.101.3.4.2.1"), # SHA-256 withRSA  <-- TLS 1.2 rsa_pkcs1_sha256
    48: ("1.2.840.113549.1.1.12",    "2.16.840.1.101.3.4.2.2"), # SHA-384 withRSA
    64: ("1.2.840.113549.1.1.13",    "2.16.840.1.101.3.4.2.3"), # SHA-512 withRSA
}

WORKDIR = os.path.dirname(os.path.abspath(__file__))


# ---- tiny HTTP helper ---------------------------------------------------------
def post(path, body, headers=None):
    url = BASE_URL + path
    data = json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "application/json")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:
        return None, "REQUEST FAILED: %s" % e


def as_json(text):
    try:
        return json.loads(text)
    except Exception:
        return None


def hr(title):
    print("\n" + "=" * 72 + "\n" + title + "\n" + "=" * 72)


def der_b64_to_pem(b64der):
    b = "".join(b64der.split())
    lines = "\n".join(b[i:i + 64] for i in range(0, len(b), 64))
    return "-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n" % lines


def find_certs(obj, acc):
    """Walk the info JSON and collect anything that looks like base64 DER certs."""
    if isinstance(obj, dict):
        for k, v in obj.items():
            if k.lower() in ("cert", "certificate", "certificates") and isinstance(v, (str, list)):
                items = v if isinstance(v, list) else [v]
                for it in items:
                    if isinstance(it, str) and len(it) > 200 and "BEGIN" not in it:
                        acc.append(it)
            find_certs(v, acc)
    elif isinstance(obj, list):
        for it in obj:
            find_certs(it, acc)


def openssl(args, stdin_bytes=None):
    return subprocess.run(["openssl"] + args, input=stdin_bytes,
                          capture_output=True)


# ------------------------------------------------------------------------------
def main():
    print(__doc__)
    print("Backend in use: %s" % BASE_URL)
    if BASE_URL == CSC_V1:
        print("NOTE: the v1 backend is OAuth2 (auth/login). This probe implements the "
              "msign v0 flow; v1 needs a username/password login step first.")

    # ---------------- STAGE 1: credentials/list --------------------------------
    hr("STAGE 1  -  credentials/list")
    print("Your Trans Sped userID is the EMAIL or PHONE registered for the cloud cert.")
    user_id = input("Trans Sped userID: ").strip()
    if not user_id:
        print("No userID entered; aborting."); return

    st, body = post("credentials/list", {"userID": user_id})
    print("HTTP %s" % st)
    j = as_json(body)
    if not j:
        print(body[:1000]); print("\nCould not parse response; aborting."); return
    print(json.dumps(j, indent=2)[:1500])
    cred_ids = j.get("credentialIDs") or j.get("credentialIds") or []
    if not cred_ids:
        print("\nNo credentials found for that userID on this backend.")
        print("If your cert is a *mobile eIDAS* cert, re-run with:")
        print("    BASE=%s python3 ts_probe.py" % CSC_V1)
        return
    if len(cred_ids) == 1:
        cid = cred_ids[0]
    else:
        for i, c in enumerate(cred_ids):
            print("  [%d] %s" % (i, c))
        cid = cred_ids[int(input("Pick credential index: ").strip())]
    print("Using credentialID: %s" % cid)

    # ---------------- STAGE 2: credentials/info --------------------------------
    hr("STAGE 2  -  credentials/info  (cert + signing constraints)")
    st, body = post("credentials/info",
                    {"credentialID": cid, "certInfo": "true", "certificates": "chain"})
    print("HTTP %s" % st)
    info = as_json(body) or {}
    print(json.dumps(info, indent=2)[:2500])

    # pull the constraint fields we care about (names vary a little by backend)
    def dig(*names):
        for n in names:
            if n in info:
                return info[n]
        return None
    scal      = dig("SCAL", "scal")
    authmode  = dig("authMode", "authmode")
    multisign = dig("multisign", "multiSign")
    print("\n-- key facts --")
    print("  SCAL (1=one auth covers many sigs; 2=OTP bound to each hash): %s" % scal)
    print("  authMode  : %s" % authmode)
    print("  multisign : %s   (max signatures one authorize can cover)" % multisign)

    certs = []
    find_certs(info, certs)
    if certs:
        leaf_pem = der_b64_to_pem(certs[0])
        open(os.path.join(WORKDIR, "leaf_cert.pem"), "w").write(leaf_pem)
        open(os.path.join(WORKDIR, "chain.pem"), "w").write(
            "".join(der_b64_to_pem(c) for c in certs))
        print("\nSaved leaf_cert.pem (+ chain.pem, %d cert(s)) to %s" % (len(certs), WORKDIR))
        r = openssl(["x509", "-noout", "-subject", "-issuer", "-serial", "-dates"],
                    leaf_pem.encode())
        print(r.stdout.decode("utf-8", "replace").strip())
        t = openssl(["x509", "-noout", "-text"], leaf_pem.encode()).stdout.decode("utf-8", "replace")
        for line in t.splitlines():
            if "Public-Key" in line or "Signature Algorithm" in line:
                print(" ", line.strip()); break
    else:
        print("\n(No embedded certificate found in the info response -- inspect the JSON above.)")

    # ---------------- STAGE 3: the signing proof (opt-in) ----------------------
    hr("STAGE 3  -  live signing proof  (sends a REAL OTP; needs your PIN)")
    print("This calls sendOTP -> authorize -> signHash over a RANDOM 32-byte hash,")
    print("then verifies the returned signature against your certificate's public key.")
    if input("Proceed? [y/N] ").strip().lower() != "y":
        print("Skipped. Stages 1-2 already confirmed reachability + cert access.")
        return

    pin = getpass.getpass("Signature PIN / password (hidden): ")
    st, body = post("credentials/sendOTP", {"credentialID": cid})
    print("sendOTP -> HTTP %s %s" % (st, (body or "")[:200]))
    otp = getpass.getpass("OTP you just received on your device (hidden): ").strip()

    digest = os.urandom(32)                      # stands in for a TLS handshake SHA-256 hash
    hb64 = base64.b64encode(digest).decode()
    sign_oid, hash_oid = OID_BY_HASHLEN[32]

    st, body = post("credentials/authorize",
                    {"credentialID": cid, "numSignatures": "1",
                     "hash": [hb64], "PIN": pin, "OTP": otp})
    print("authorize -> HTTP %s" % st)
    aj = as_json(body) or {}
    sad = aj.get("SAD") or aj.get("sad")
    if not sad:
        print(json.dumps(aj, indent=2)[:1200] if aj else body[:1200])
        print("\nNo SAD returned -- authorize failed (check PIN/OTP). Stopping."); return
    print("Got SAD (len %d)." % len(sad))

    st, body = post("signatures/signHash",
                    {"credentialID": cid, "signAlgo": sign_oid, "hashAlgo": hash_oid,
                     "signAlgoParams": "", "SAD": sad, "hash": [hb64]})
    print("signHash -> HTTP %s" % st)
    sj = as_json(body) or {}
    sigs = sj.get("signatures") or sj.get("SignatureObject") or []
    if isinstance(sigs, str):
        sigs = [sigs]
    if not sigs:
        print(json.dumps(sj, indent=2)[:1200] if sj else body[:1200])
        print("\nNo signature returned. Stopping."); return
    sig = base64.b64decode(sigs[0])
    print("Signature received: %d bytes (RSA-2048 => expect 256)." % len(sig))

    # ---- verify the signature against the cert's public key -------------------
    hr("VERIFY  -  does the signature match the certificate's public key?")
    ok = None
    try:
        from cryptography.hazmat.primitives.asymmetric import padding, utils
        from cryptography.hazmat.primitives import hashes
        from cryptography import x509
        pub = x509.load_pem_x509_certificate(open(os.path.join(WORKDIR, "leaf_cert.pem"), "rb").read()).public_key()
        pub.verify(sig, digest, padding.PKCS1v15(), utils.Prehashed(hashes.SHA256()))
        ok = True
    except ImportError:
        # openssl fallback: verify PKCS#1 v1.5 over the prehashed SHA-256 digest
        pub = openssl(["x509", "-in", os.path.join(WORKDIR, "leaf_cert.pem"),
                       "-pubkey", "-noout"]).stdout
        open(os.path.join(WORKDIR, "_pub.pem"), "wb").write(pub)
        open(os.path.join(WORKDIR, "_sig.bin"), "wb").write(sig)
        open(os.path.join(WORKDIR, "_dig.bin"), "wb").write(digest)
        r = openssl(["pkeyutl", "-verify", "-pubin", "-inkey", os.path.join(WORKDIR, "_pub.pem"),
                     "-sigfile", os.path.join(WORKDIR, "_sig.bin"),
                     "-in", os.path.join(WORKDIR, "_dig.bin"),
                     "-pkeyopt", "rsa_padding_mode:pkcs1", "-pkeyopt", "digest:sha256"])
        out = (r.stdout + r.stderr).decode("utf-8", "replace").strip()
        ok = (r.returncode == 0 and "Success" in out)
        print("openssl:", out)
        for f in ("_pub.pem", "_sig.bin", "_dig.bin"):
            try: os.remove(os.path.join(WORKDIR, f))
            except OSError: pass
    except Exception as e:
        print("verify error:", e)

    hr("VERDICT")
    if ok:
        print("PASS  ✅  The Trans Sped cloud signed an arbitrary hash and the signature")
        print("verifies against your certificate. This is exactly the TLS client-cert")
        print("operation ANAF login needs -> Approach A (Firefox + PKCS#11) is confirmed viable.")
        print("\nUX note: SCAL=%s. If SCAL=2, each TLS handshake needs a fresh OTP;" % scal)
        print("if SCAL=1, one authorize (numSignatures>1) can cover a whole login.")
    else:
        print("Signature verification did NOT pass. Share the HTTP statuses/bodies above")
        print("(redact anything personal) and we'll diagnose.")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\nInterrupted.")
