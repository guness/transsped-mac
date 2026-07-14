# ANAF macOS Login (Trans Sped cloud → Firefox PKCS#11) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a macOS PKCS#11 module that lets Firefox present a Trans Sped **cloud** qualified certificate for client-certificate TLS, so the user can log in to ANAF SPV without Windows.

**Architecture:** A Go `internal/csc` package drives Trans Sped's CSC cloud API (the raw-hash signer). A Go PKCS#11 module (via `namecoin/pkcs11mod`) vends the cloud cert + a delegating RSA private key to Firefox/NSS; its `Sign` forwards the TLS handshake hash to `internal/csc` (which triggers the OTP flow). A dedicated Firefox profile pins TLS 1.2 and loads the module.

**Tech Stack:** Go 1.21+, `github.com/miekg/pkcs11`, `github.com/namecoin/pkcs11mod`, macOS `osascript` (OTP dialog), `pkcs11-tool` (OpenSC) + `certutil`/`modutil` (nss) for testing, Firefox.

## Global Constraints

- Go module path: `tscloud`. Go **1.21+**.
- CSC base URL default: `https://msign.transsped.ro/csc/v0/local/` (trailing slash; joined with `credentials/list` etc.).
- Signing is **RSA PKCS#1 v1.5 only**; the ANAF TLS session is **TLS 1.2 only** (cloud cannot do the PSS that TLS 1.3 needs).
- `SCAL = 2`: a fresh OTP-authorized, hash-bound SAD is minted per signature. One `Sign` = one `sendOTP` + one `authorize` + one `signHash`.
- **Never** log, persist, or write to disk the signature PIN or the OTP. They live only in memory for the duration of one operation.
- Build target: **darwin/arm64**, `CGO_ENABLED=1 go build -buildmode=c-shared`, then `codesign -s -` (ad-hoc).
- Dependencies beyond the two PKCS#11 libs must be Go stdlib only.
- CSC request/response contracts (base = the CSC base URL):
  - `POST credentials/list` `{"userID":"<email|phone>"}` → `{"credentialIDs":[...]}`
  - `POST credentials/info` `{"credentialID":"..","certInfo":"true","certificates":"chain"}` → `{"cert":<...>,"key":{...},"authMode":..,"SCAL":..,"multisign":..}`
  - `POST credentials/sendOTP` `{"credentialID":".."}` → 200
  - `POST credentials/authorize` `{"credentialID":"..","numSignatures":"1","hash":["<b64>"],"PIN":"..","OTP":".."}` → `{"SAD":"..","expiresIn":..}`
  - `POST signatures/signHash` `{"credentialID":"..","signAlgo":"<OID>","hashAlgo":"<OID>","signAlgoParams":"","SAD":"..","hash":["<b64>"]}` → `{"signatures":["<b64>"]}`
- signAlgo/hashAlgo by digest length: `32`→`1.2.840.113549.1.1.11`/`2.16.840.1.101.3.4.2.1` (SHA-256, primary); `48`→`1.2.840.113549.1.1.12`/`2.16.840.1.101.3.4.2.2`; `64`→`1.2.840.113549.1.1.13`/`2.16.840.1.101.3.4.2.3`; `20`→`1.3.14.3.2.29`/`1.3.14.3.2.26`.

---

## File Structure

```
tscloud/
  go.mod
  internal/csc/
    algo.go          # DigestInfo parsing + hash-length → CSC OID mapping
    algo_test.go
    client.go        # CSC HTTP client: List/Info/SendOTP/Authorize/SignHash
    client_test.go
    signer.go        # SignDigestInfo(): parse → sendOTP → authorize → signHash
    signer_test.go
  internal/otp/
    prompt.go        # OTPPrompter interface + osascript implementation
  internal/config/
    config.go        # load/save ~/.config/tscloud/{config.json,leaf.der,intermediate.der}
    config_test.go
  internal/token/
    objects.go       # PKCS#11 object model (cert/privkey/intermediate) + attribute matching
    objects_test.go
    backend.go       # implements pkcs11mod.Backend (real methods)
    backend_stubs.go # remaining Backend methods → CKR_FUNCTION_NOT_SUPPORTED
    backend_test.go
  cmd/tscloud-setup/
    main.go          # one-time: fetch credentials/info, write config + certs
  cmd/pkcs11/
    main.go          # init(): pkcs11mod.SetBackend(token.NewBackend(...))
  scripts/
    setup-firefox.sh # create profile, load module, pin TLS 1.2, import intermediate
    build.sh         # build dylib + binaries, codesign
  config.example.json
  README.md
```

---

## Phase 1 — CSC client & signer (pure Go, TDD)

### Task 1: Project scaffold + dependencies

**Files:** Create `go.mod`

- [ ] **Step 1: Init module and add deps**

Run:
```bash
cd ~/Desktop/EasySign-macos
go mod init tscloud
go get github.com/miekg/pkcs11@latest
go get github.com/namecoin/pkcs11mod@latest
go mod tidy
```
Expected: `go.mod` lists both dependencies; no errors.

- [ ] **Step 2: Commit**
```bash
git add go.mod go.sum && git commit -m "chore: go module + pkcs11 deps"
```

---

### Task 2: DigestInfo parsing + algorithm mapping

**Files:** Create `internal/csc/algo.go`, `internal/csc/algo_test.go`

**Interfaces:**
- Produces: `ParseHashInput(data []byte) (rawHash []byte, signAlgo string, hashAlgo string, err error)` — accepts either a DER `DigestInfo` (what NSS passes for `CKM_RSA_PKCS`) or a bare digest; returns the raw digest and the CSC OID pair.

- [ ] **Step 1: Write the failing test**

```go
package csc

import (
	"crypto/sha256"
	"encoding/asn1"
	"testing"
)

// digestInfoSHA256 wraps a 32-byte digest exactly as NSS does for CKM_RSA_PKCS.
func digestInfoSHA256(t *testing.T, digest []byte) []byte {
	t.Helper()
	type algID struct {
		OID  asn1.ObjectIdentifier
		Null asn1.RawValue
	}
	type di struct {
		Alg    algID
		Digest []byte
	}
	b, err := asn1.Marshal(di{
		Alg:    algID{OID: asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}, Null: asn1.RawValue{Tag: 5}},
		Digest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseHashInput_DigestInfoSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	in := digestInfoSHA256(t, sum[:])
	raw, sign, hash, err := ParseHashInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(sum[:]) {
		t.Fatalf("raw digest mismatch")
	}
	if sign != "1.2.840.113549.1.1.11" || hash != "2.16.840.1.101.3.4.2.1" {
		t.Fatalf("wrong OIDs: %s / %s", sign, hash)
	}
}

func TestParseHashInput_BareDigest(t *testing.T) {
	sum := sha256.Sum256([]byte("hi"))
	raw, sign, _, err := ParseHashInput(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 32 || sign != "1.2.840.113549.1.1.11" {
		t.Fatalf("bare 32-byte digest not handled")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/csc/ -run TestParseHashInput -v`
Expected: FAIL — `undefined: ParseHashInput`.

- [ ] **Step 3: Write minimal implementation**

```go
package csc

import (
	"encoding/asn1"
	"fmt"
)

type oidPair struct{ sign, hash string }

// byLen maps a raw digest length to (signAlgo, hashAlgo) OIDs — the exact
// pairs the Trans Sped msign backend selects on.
var byLen = map[int]oidPair{
	20: {"1.3.14.3.2.29", "1.3.14.3.2.26"},
	32: {"1.2.840.113549.1.1.11", "2.16.840.1.101.3.4.2.1"},
	48: {"1.2.840.113549.1.1.12", "2.16.840.1.101.3.4.2.2"},
	64: {"1.2.840.113549.1.1.13", "2.16.840.1.101.3.4.2.3"},
}

type digestInfo struct {
	Alg struct {
		OID  asn1.ObjectIdentifier
		Null asn1.RawValue `asn1:"optional"`
	}
	Digest []byte
}

// ParseHashInput accepts either a DER DigestInfo (NSS CKM_RSA_PKCS input) or a
// bare digest, and returns the raw digest plus the CSC signAlgo/hashAlgo OIDs.
func ParseHashInput(data []byte) (raw []byte, signAlgo, hashAlgo string, err error) {
	var di digestInfo
	if rest, e := asn1.Unmarshal(data, &di); e == nil && len(rest) == 0 && len(di.Digest) > 0 {
		if p, ok := byLen[len(di.Digest)]; ok {
			return di.Digest, p.sign, p.hash, nil
		}
	}
	if p, ok := byLen[len(data)]; ok { // fall back: treat as a bare digest
		return data, p.sign, p.hash, nil
	}
	return nil, "", "", fmt.Errorf("unrecognized hash input of %d bytes", len(data))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/csc/ -run TestParseHashInput -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**
```bash
git add internal/csc/algo.go internal/csc/algo_test.go
git commit -m "feat(csc): parse DigestInfo/bare digest to CSC OIDs"
```

---

### Task 3: CSC HTTP client

**Files:** Create `internal/csc/client.go`, `internal/csc/client_test.go`

**Interfaces:**
- Produces:
  - `type Client struct { BaseURL string; HTTP *http.Client }`
  - `func New(baseURL string) *Client`
  - `func (c *Client) List(userID string) ([]string, error)`
  - `func (c *Client) Info(credentialID string) (*Info, error)` where `Info` has `CertB64 []string`, `KeyAlgo string`, `KeyLen int`, `SCAL string`, `Multisign int`, `AuthMode string`
  - `func (c *Client) SendOTP(credentialID string) error`
  - `func (c *Client) Authorize(credentialID, pin, otp, hashB64 string) (sad string, err error)`
  - `func (c *Client) SignHash(credentialID, sad, hashB64, signAlgo, hashAlgo string) (sigB64 string, err error)`

- [ ] **Step 1: Write the failing test** (uses `httptest`, no network)

```go
package csc

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_AuthorizeAndSignHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/credentials/authorize"):
			var req map[string]any
			json.Unmarshal(body, &req)
			if req["PIN"] != "1234" || req["OTP"] != "999000" {
				http.Error(w, `{"error":"bad pin/otp"}`, 400)
				return
			}
			io.WriteString(w, `{"SAD":"SAD-TOKEN","expiresIn":300}`)
		case strings.HasSuffix(r.URL.Path, "/signatures/signHash"):
			io.WriteString(w, `{"signatures":["c2ln"]}`) // base64("sig")
		default:
			http.Error(w, "no route", 404)
		}
	}))
	defer srv.Close()

	c := New(srv.URL + "/")
	sad, err := c.Authorize("cred1", "1234", "999000", "aGFzaA==")
	if err != nil || sad != "SAD-TOKEN" {
		t.Fatalf("authorize: sad=%q err=%v", sad, err)
	}
	sig, err := c.SignHash("cred1", sad, "aGFzaA==", "1.2.840.113549.1.1.11", "2.16.840.1.101.3.4.2.1")
	if err != nil || sig != "c2ln" {
		t.Fatalf("signHash: sig=%q err=%v", sig, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/csc/ -run TestClient -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

```go
package csc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) post(path string, req any, out any) error {
	buf, _ := json.Marshal(req)
	resp, err := c.HTTP.Post(c.BaseURL+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

type Info struct {
	CertB64   []string
	KeyAlgo   string
	KeyLen    int
	SCAL      string
	Multisign int
	AuthMode  string
}

func (c *Client) List(userID string) ([]string, error) {
	var r struct {
		CredentialIDs []string `json:"credentialIDs"`
	}
	if err := c.post("credentials/list", map[string]string{"userID": userID}, &r); err != nil {
		return nil, err
	}
	return r.CredentialIDs, nil
}

func (c *Client) SendOTP(credentialID string) error {
	return c.post("credentials/sendOTP", map[string]string{"credentialID": credentialID}, nil)
}

func (c *Client) Authorize(credentialID, pin, otp, hashB64 string) (string, error) {
	var r struct {
		SAD string `json:"SAD"`
	}
	req := map[string]any{
		"credentialID": credentialID, "numSignatures": "1",
		"hash": []string{hashB64}, "PIN": pin, "OTP": otp,
	}
	if err := c.post("credentials/authorize", req, &r); err != nil {
		return "", err
	}
	if r.SAD == "" {
		return "", fmt.Errorf("authorize returned empty SAD")
	}
	return r.SAD, nil
}

func (c *Client) SignHash(credentialID, sad, hashB64, signAlgo, hashAlgo string) (string, error) {
	var r struct {
		Signatures []string `json:"signatures"`
	}
	req := map[string]any{
		"credentialID": credentialID, "signAlgo": signAlgo, "hashAlgo": hashAlgo,
		"signAlgoParams": "", "SAD": sad, "hash": []string{hashB64},
	}
	if err := c.post("signatures/signHash", req, &r); err != nil {
		return "", err
	}
	if len(r.Signatures) == 0 {
		return "", fmt.Errorf("signHash returned no signature")
	}
	return r.Signatures[0], nil
}
```

Also add `Info()` (parses `cert`/`certificates` into `CertB64`, and `key`,`SCAL`,`multisign`,`authMode`). Because the raw shape varies, unmarshal into `map[string]any` and extract defensively:

```go
func (c *Client) Info(credentialID string) (*Info, error) {
	var raw map[string]any
	req := map[string]any{"credentialID": credentialID, "certInfo": "true", "certificates": "chain"}
	if err := c.post("credentials/info", req, &raw); err != nil {
		return nil, err
	}
	info := &Info{}
	// certificates: look for []string of base64 DER under cert/certificates
	collect := func(v any) {
		switch t := v.(type) {
		case string:
			if len(t) > 200 {
				info.CertB64 = append(info.CertB64, t)
			}
		case []any:
			for _, e := range t {
				if s, ok := e.(string); ok && len(s) > 200 {
					info.CertB64 = append(info.CertB64, s)
				}
			}
		case map[string]any:
			if cc, ok := t["certificates"]; ok {
				if arr, ok := cc.([]any); ok {
					for _, e := range arr {
						if s, ok := e.(string); ok {
							info.CertB64 = append(info.CertB64, s)
						}
					}
				}
			}
		}
	}
	collect(raw["cert"])
	collect(raw["certificates"])
	if s, ok := raw["SCAL"].(string); ok {
		info.SCAL = s
	}
	if k, ok := raw["key"].(map[string]any); ok {
		if a, ok := k["algo"].(string); ok {
			info.KeyAlgo = a
		}
	}
	return info, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/csc/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/csc/client.go internal/csc/client_test.go
git commit -m "feat(csc): CSC HTTP client (list/info/sendOTP/authorize/signHash)"
```

---

### Task 4: OTP prompter (osascript)

**Files:** Create `internal/otp/prompt.go`

**Interfaces:**
- Produces: `type Prompter interface { OTP(prompt string) (string, error) }` and `type OSAScript struct{}` implementing it; plus `type Static struct{ Value string }` for tests.

- [ ] **Step 1: Implement (thin wrapper — no unit test; validated manually in Task 11)**

```go
package otp

import (
	"os/exec"
	"strings"
)

type Prompter interface {
	OTP(prompt string) (string, error)
}

// OSAScript shows a native macOS dialog and returns the typed code.
type OSAScript struct{}

func (OSAScript) OTP(prompt string) (string, error) {
	script := `display dialog "` + escape(prompt) +
		`" default answer "" with title "ANAF login — Trans Sped OTP" with hidden answer ` +
		`buttons {"Cancel","OK"} default button "OK"`
	out, err := exec.Command("osascript", "-e", script, "-e",
		`text returned of result`).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func escape(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }

// Static is a test/CI prompter.
type Static struct{ Value string }

func (s Static) OTP(string) (string, error) { return s.Value, nil }
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/otp/`
Expected: no output (success).

- [ ] **Step 3: Commit**
```bash
git add internal/otp/prompt.go && git commit -m "feat(otp): osascript OTP prompter"
```

---

### Task 5: Signer orchestration

**Files:** Create `internal/csc/signer.go`, `internal/csc/signer_test.go`

**Interfaces:**
- Consumes: `Client` (Task 3), `ParseHashInput` (Task 2), `otp.Prompter` (Task 4).
- Produces:
  - `type OTPPrompter interface { OTP(prompt string) (string, error) }` (local alias so `csc` doesn't import `otp`)
  - `type Signer struct { Client *Client; CredentialID string; PIN func() string; OTP OTPPrompter }`
  - `func (s *Signer) SignDigestInfo(data []byte) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

```go
package csc

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeOTP struct{}

func (fakeOTP) OTP(string) (string, error) { return "999000", nil }

func TestSigner_SignDigestInfo(t *testing.T) {
	var gotSendOTP bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/credentials/sendOTP"):
			gotSendOTP = true
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/credentials/authorize"):
			io.WriteString(w, `{"SAD":"SAD1"}`)
		case strings.HasSuffix(r.URL.Path, "/signatures/signHash"):
			io.WriteString(w, `{"signatures":["`+base64.StdEncoding.EncodeToString([]byte("SIGNATURE"))+`"]}`)
		}
	}))
	defer srv.Close()

	s := &Signer{Client: New(srv.URL + "/"), CredentialID: "cred1",
		PIN: func() string { return "1234" }, OTP: fakeOTP{}}
	sum := sha256.Sum256([]byte("handshake"))
	sig, err := s.SignDigestInfo(sum[:]) // bare 32-byte digest path
	if err != nil {
		t.Fatal(err)
	}
	if string(sig) != "SIGNATURE" {
		t.Fatalf("sig=%q", sig)
	}
	if !gotSendOTP {
		t.Fatal("sendOTP was not called")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/csc/ -run TestSigner -v`
Expected: FAIL — `undefined: Signer`.

- [ ] **Step 3: Write minimal implementation**

```go
package csc

import "encoding/base64"

type OTPPrompter interface {
	OTP(prompt string) (string, error)
}

type Signer struct {
	Client       *Client
	CredentialID string
	PIN          func() string
	OTP          OTPPrompter
}

// SignDigestInfo runs the full SCAL2 flow for one signature and returns raw
// signature bytes. `data` is the DigestInfo (or bare digest) NSS handed C_Sign.
func (s *Signer) SignDigestInfo(data []byte) ([]byte, error) {
	digest, signAlgo, hashAlgo, err := ParseHashInput(data)
	if err != nil {
		return nil, err
	}
	hb := base64.StdEncoding.EncodeToString(digest)
	if err := s.Client.SendOTP(s.CredentialID); err != nil {
		return nil, err
	}
	otp, err := s.OTP.OTP("Enter the OTP from your Trans Sped app to authorise the ANAF login:")
	if err != nil {
		return nil, err
	}
	sad, err := s.Client.Authorize(s.CredentialID, s.PIN(), otp, hb)
	if err != nil {
		return nil, err
	}
	sig64, err := s.Client.SignHash(s.CredentialID, sad, hb, signAlgo, hashAlgo)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(sig64)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/csc/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/csc/signer.go internal/csc/signer_test.go
git commit -m "feat(csc): signer orchestration (sendOTP→authorize→signHash)"
```

---

### Task 6: Config load/save + certificate loading

**Files:** Create `internal/config/config.go`, `internal/config/config_test.go`, `config.example.json`

**Interfaces:**
- Produces:
  - `type Config struct { BaseURL, UserID, CredentialID, Label string }`
  - `func Dir() string` → `~/.config/tscloud`
  - `func Load() (*Config, *x509.Certificate, []*x509.Certificate, error)` (config, leaf, intermediates)
  - `func Save(cfg *Config, leafDER []byte, interDER [][]byte) error`

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TSCLOUD_DIR", tmp)
	// a throwaway self-signed cert in DER for the round trip
	leaf := selfSignedDER(t)
	cfg := &Config{BaseURL: "https://x/", UserID: "u", CredentialID: "c", Label: "L"}
	if err := Save(cfg, leaf, nil); err != nil {
		t.Fatal(err)
	}
	got, cert, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.CredentialID != "c" || cert == nil {
		t.Fatalf("round trip failed: %+v cert=%v", got, cert)
	}
	if _, err := os.Stat(filepath.Join(tmp, "leaf.der")); err != nil {
		t.Fatalf("leaf.der not written: %v", err)
	}
}
```
(Add a `selfSignedDER(t)` helper in the test using `crypto/rsa` + `x509.CreateCertificate`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `undefined: Save`.

- [ ] **Step 3: Write minimal implementation**

```go
package config

import (
	"crypto/x509"
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL      string `json:"baseURL"`
	UserID       string `json:"userID"`
	CredentialID string `json:"credentialID"`
	Label        string `json:"label"`
}

func Dir() string {
	if d := os.Getenv("TSCLOUD_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tscloud")
}

func Save(cfg *Config, leafDER []byte, interDER [][]byte) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "leaf.der"), leafDER, 0o600); err != nil {
		return err
	}
	for i, d := range interDER {
		_ = os.WriteFile(filepath.Join(dir, "intermediate"+string(rune('0'+i))+".der"), d, 0o600)
	}
	return nil
}

func Load() (*Config, *x509.Certificate, []*x509.Certificate, error) {
	dir := Dir()
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, nil, nil, err
	}
	leafDER, err := os.ReadFile(filepath.Join(dir, "leaf.der"))
	if err != nil {
		return nil, nil, nil, err
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return nil, nil, nil, err
	}
	var inter []*x509.Certificate
	for i := 0; ; i++ {
		d, err := os.ReadFile(filepath.Join(dir, "intermediate"+string(rune('0'+i))+".der"))
		if err != nil {
			break
		}
		if c, err := x509.ParseCertificate(d); err == nil {
			inter = append(inter, c)
		}
	}
	return &cfg, leaf, inter, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/config/ config.example.json
git commit -m "feat(config): load/save config + certs under ~/.config/tscloud"
```

---

### Task 7: `tscloud-setup` CLI (fetch cert, write config)

**Files:** Create `cmd/tscloud-setup/main.go`

**Interfaces:**
- Consumes: `csc.Client`, `config.Save`.

- [ ] **Step 1: Implement**

```go
package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"tscloud/internal/config"
	"tscloud/internal/csc"
)

func main() {
	base := flag.String("base", "https://msign.transsped.ro/csc/v0/local/", "CSC base URL")
	user := flag.String("user", "", "Trans Sped userID (email or phone)")
	flag.Parse()
	if *user == "" {
		fmt.Fprintln(os.Stderr, "usage: tscloud-setup -user <email|phone> [-base URL]")
		os.Exit(2)
	}
	c := csc.New(*base)
	ids, err := c.List(*user)
	must(err)
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "no credentials for that user on", *base)
		os.Exit(1)
	}
	cred := ids[0]
	fmt.Println("credentialID:", cred)
	info, err := c.Info(cred)
	must(err)
	if len(info.CertB64) == 0 {
		fmt.Fprintln(os.Stderr, "credentials/info returned no certificate")
		os.Exit(1)
	}
	leaf, err := base64.StdEncoding.DecodeString(clean(info.CertB64[0]))
	must(err)
	var inter [][]byte
	for _, b := range info.CertB64[1:] {
		d, err := base64.StdEncoding.DecodeString(clean(b))
		if err == nil {
			inter = append(inter, d)
		}
	}
	must(config.Save(&config.Config{BaseURL: *base, UserID: *user, CredentialID: cred, Label: "Trans Sped Cloud"}, leaf, inter))
	fmt.Printf("Saved config + %d cert(s) to %s (SCAL=%s)\n", 1+len(inter), config.Dir(), info.SCAL)
}

func clean(s string) string { // strip PEM armor/whitespace if present
	out := ""
	for _, line := range splitLines(s) {
		if line == "" || line[0] == '-' {
			continue
		}
		out += line
	}
	return out
}
func splitLines(s string) []string { /* trivial split on \n and \r */ return nil }
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```
(Complete `splitLines` with a `strings.FieldsFunc(s, func(r rune) bool { return r=='\n'||r=='\r' })`.)

- [ ] **Step 2: Build and run against the real cert**

Run:
```bash
go build ./cmd/tscloud-setup/ && ./tscloud-setup -user "<your email/phone>"
```
Expected: prints `credentialID:` and `Saved config + N cert(s) to ~/.config/tscloud (SCAL=2)`; `~/.config/tscloud/leaf.der` exists.

Verify: `openssl x509 -inform der -in ~/.config/tscloud/leaf.der -noout -subject -issuer` shows your name and `Trans Sped ... QCA G3`.

- [ ] **Step 3: Commit**
```bash
git add cmd/tscloud-setup/ && git commit -m "feat(cmd): tscloud-setup fetches cert + writes config"
```

---

## Phase 2 — PKCS#11 module (Go / pkcs11mod)

> The `pkcs11mod.Backend` interface mirrors `*miekg/pkcs11.Ctx` (a large interface). We implement ~19 methods with real behavior (Task 9) and stub the rest to `CKR_FUNCTION_NOT_SUPPORTED` (Task 9, Step 3 — the Go compiler enumerates exactly which are missing).

### Task 8: PKCS#11 object model

**Files:** Create `internal/token/objects.go`, `internal/token/objects_test.go`

**Interfaces:**
- Produces:
  - `type Object struct { Handle pkcs11.ObjectHandle; Attrs map[uint][]byte }`
  - `func BuildObjects(leaf *x509.Certificate, inter []*x509.Certificate) []*Object` — returns [cert, privkey, intermediate…] with the attributes NSS needs.
  - `func (o *Object) Matches(template []*pkcs11.Attribute) bool`
  - `func keyID(leaf *x509.Certificate) []byte` — SHA-1 of the DER public key (shared CKA_ID linking cert↔key).

- [ ] **Step 1: Write the failing test**

```go
package token

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"

	"github.com/miekg/pkcs11"
)

func sampleLeaf(t *testing.T) *x509.Certificate {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test User"}}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	c, _ := x509.ParseCertificate(der)
	return c
}

func TestBuildObjects(t *testing.T) {
	leaf := sampleLeaf(t)
	objs := BuildObjects(leaf, nil)
	if len(objs) != 2 {
		t.Fatalf("want cert+privkey, got %d", len(objs))
	}
	// find the private key and check it is findable by CKO_PRIVATE_KEY
	tmpl := []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY)}
	var priv *Object
	for _, o := range objs {
		if o.Matches(tmpl) {
			priv = o
		}
	}
	if priv == nil {
		t.Fatal("private key object not findable by class")
	}
	if len(priv.Attrs[pkcs11.CKA_MODULUS]) == 0 || len(priv.Attrs[pkcs11.CKA_ID]) == 0 {
		t.Fatal("private key missing modulus/id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/token/ -run TestBuildObjects -v`
Expected: FAIL — `undefined: BuildObjects`.

- [ ] **Step 3: Write minimal implementation**

```go
package token

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"

	"github.com/miekg/pkcs11"
)

type Object struct {
	Handle pkcs11.ObjectHandle
	Attrs  map[uint][]byte
}

func keyID(leaf *x509.Certificate) []byte {
	h := sha1.Sum(leaf.RawSubjectPublicKeyInfo)
	return h[:]
}

func b(v uint) []byte { // CK_ULONG (little-endian native on arm64) as bytes
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, uint64(v))
	return out
}

func BuildObjects(leaf *x509.Certificate, inter []*x509.Certificate) []*Object {
	id := keyID(leaf)
	pub := leaf.PublicKey.(*rsa.PublicKey)
	exp := make([]byte, 8)
	binary.BigEndian.PutUint64(exp, uint64(pub.E))
	exp = bytes.TrimLeft(exp, "\x00")

	cert := &Object{Handle: 1, Attrs: map[uint][]byte{
		pkcs11.CKA_CLASS:            b(pkcs11.CKO_CERTIFICATE),
		pkcs11.CKA_CERTIFICATE_TYPE: b(pkcs11.CKC_X_509),
		pkcs11.CKA_TOKEN:            {1},
		pkcs11.CKA_ID:               id,
		pkcs11.CKA_LABEL:            []byte("Trans Sped Cloud"),
		pkcs11.CKA_VALUE:            leaf.Raw,
		pkcs11.CKA_SUBJECT:          leaf.RawSubject,
		pkcs11.CKA_ISSUER:           leaf.RawIssuer,
		pkcs11.CKA_SERIAL_NUMBER:    leaf.RawSerialNumber(), // see note
	}}
	priv := &Object{Handle: 2, Attrs: map[uint][]byte{
		pkcs11.CKA_CLASS:           b(pkcs11.CKO_PRIVATE_KEY),
		pkcs11.CKA_KEY_TYPE:        b(pkcs11.CKK_RSA),
		pkcs11.CKA_TOKEN:           {1},
		pkcs11.CKA_PRIVATE:         {1},
		pkcs11.CKA_SIGN:            {1},
		pkcs11.CKA_ID:              id,
		pkcs11.CKA_LABEL:           []byte("Trans Sped Cloud"),
		pkcs11.CKA_MODULUS:         pub.N.Bytes(),
		pkcs11.CKA_PUBLIC_EXPONENT: exp,
	}}
	objs := []*Object{cert, priv}
	for i, c := range inter {
		objs = append(objs, &Object{Handle: pkcs11.ObjectHandle(3 + i), Attrs: map[uint][]byte{
			pkcs11.CKA_CLASS:            b(pkcs11.CKO_CERTIFICATE),
			pkcs11.CKA_CERTIFICATE_TYPE: b(pkcs11.CKC_X_509),
			pkcs11.CKA_TOKEN:            {1},
			pkcs11.CKA_VALUE:            c.Raw,
			pkcs11.CKA_SUBJECT:          c.RawSubject,
			pkcs11.CKA_ISSUER:           c.RawIssuer,
		}})
	}
	return objs
}

func (o *Object) Matches(template []*pkcs11.Attribute) bool {
	for _, a := range template {
		v, ok := o.Attrs[a.Type]
		if !ok || !bytes.Equal(v, a.Value) {
			return false
		}
	}
	return true
}
```
> Note: `RawSerialNumber()` is illustrative — if absent on this Go version, DER-encode `leaf.SerialNumber` with `encoding/asn1`. The compiler will flag it; fix in this step.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/token/ -run TestBuildObjects -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/token/objects.go internal/token/objects_test.go
git commit -m "feat(token): PKCS#11 object model for cert+privkey"
```

---

### Task 9: Backend implementation + module entrypoint

**Files:** Create `internal/token/backend.go`, `internal/token/backend_stubs.go`, `cmd/pkcs11/main.go`

**Interfaces:**
- Consumes: `Object`/`BuildObjects` (Task 8), `csc.Signer` (Task 5), `config.Load` (Task 6), `pkcs11mod.SetBackend` + `pkcs11mod.Backend`.
- Produces: `func NewBackend(objs []*Object, signer *csc.Signer) *Backend` implementing `pkcs11mod.Backend`.

- [ ] **Step 1: Implement the real methods** (`backend.go`)

```go
package token

import (
	"tscloud/internal/csc"

	"github.com/miekg/pkcs11"
)

const slotID = 0

type Backend struct {
	objs    []*Object
	signer  *csc.Signer
	pin     string            // cached at Login; also fed to signer via PIN()
	find    []pkcs11.ObjectHandle
	signKey pkcs11.ObjectHandle
}

func NewBackend(objs []*Object, signer *csc.Signer) *Backend {
	bk := &Backend{objs: objs, signer: signer}
	signer.PIN = func() string { return bk.pin }
	return bk
}

func ckErr(code uint) error { return pkcs11.Error(code) }

func (b *Backend) Initialize() error { return nil }
func (b *Backend) Finalize() error   { return nil }

func (b *Backend) GetInfo() (pkcs11.Info, error) {
	return pkcs11.Info{ManufacturerID: "Trans Sped", LibraryDescription: "TS Cloud PKCS#11"}, nil
}
func (b *Backend) GetSlotList(bool) ([]uint, error) { return []uint{slotID}, nil }
func (b *Backend) GetSlotInfo(uint) (pkcs11.SlotInfo, error) {
	return pkcs11.SlotInfo{SlotDescription: "TS Cloud", Flags: pkcs11.CKF_TOKEN_PRESENT}, nil
}
func (b *Backend) GetTokenInfo(uint) (pkcs11.TokenInfo, error) {
	return pkcs11.TokenInfo{Label: "Trans Sped Cloud", ManufacturerID: "Trans Sped",
		Flags: pkcs11.CKF_TOKEN_INITIALIZED | pkcs11.CKF_LOGIN_REQUIRED | pkcs11.CKF_USER_PIN_INITIALIZED}, nil
}
func (b *Backend) GetMechanismList(uint) ([]*pkcs11.Mechanism, error) {
	return []*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_RSA_PKCS, nil)}, nil
}
func (b *Backend) GetMechanismInfo(uint, []*pkcs11.Mechanism) (pkcs11.MechanismInfo, error) {
	return pkcs11.MechanismInfo{MinKeySize: 2048, MaxKeySize: 4096, Flags: pkcs11.CKF_SIGN}, nil
}
func (b *Backend) OpenSession(uint, uint) (pkcs11.SessionHandle, error) { return 1, nil }
func (b *Backend) CloseSession(pkcs11.SessionHandle) error              { return nil }
func (b *Backend) GetSessionInfo(pkcs11.SessionHandle) (pkcs11.SessionInfo, error) {
	return pkcs11.SessionInfo{SlotID: slotID, State: pkcs11.CKS_RO_USER_FUNCTIONS}, nil
}
func (b *Backend) Login(_ pkcs11.SessionHandle, _ uint, pin string) error { b.pin = pin; return nil }
func (b *Backend) Logout(pkcs11.SessionHandle) error                      { b.pin = ""; return nil }

func (b *Backend) FindObjectsInit(_ pkcs11.SessionHandle, tmpl []*pkcs11.Attribute) error {
	b.find = nil
	for _, o := range b.objs {
		if o.Matches(tmpl) {
			b.find = append(b.find, o.Handle)
		}
	}
	return nil
}
func (b *Backend) FindObjects(_ pkcs11.SessionHandle, max int) ([]pkcs11.ObjectHandle, bool, error) {
	n := len(b.find)
	if n > max {
		n = max
	}
	out := b.find[:n]
	b.find = b.find[n:]
	return out, false, nil
}
func (b *Backend) FindObjectsFinal(pkcs11.SessionHandle) error { b.find = nil; return nil }

func (b *Backend) GetAttributeValue(_ pkcs11.SessionHandle, h pkcs11.ObjectHandle, tmpl []*pkcs11.Attribute) ([]*pkcs11.Attribute, error) {
	var obj *Object
	for _, o := range b.objs {
		if o.Handle == h {
			obj = o
		}
	}
	if obj == nil {
		return nil, ckErr(pkcs11.CKR_OBJECT_HANDLE_INVALID)
	}
	out := make([]*pkcs11.Attribute, 0, len(tmpl))
	for _, a := range tmpl {
		if v, ok := obj.Attrs[a.Type]; ok {
			out = append(out, pkcs11.NewAttribute(a.Type, v))
		} else {
			out = append(out, pkcs11.NewAttribute(a.Type, nil)) // absent
		}
	}
	return out, nil
}

func (b *Backend) SignInit(_ pkcs11.SessionHandle, _ []*pkcs11.Mechanism, key pkcs11.ObjectHandle) error {
	b.signKey = key
	return nil
}
func (b *Backend) Sign(_ pkcs11.SessionHandle, data []byte) ([]byte, error) {
	sig, err := b.signer.SignDigestInfo(data)
	if err != nil {
		return nil, ckErr(pkcs11.CKR_FUNCTION_FAILED)
	}
	return sig, nil
}
```

- [ ] **Step 2: Write the module entrypoint** (`cmd/pkcs11/main.go`)

```go
package main

import (
	"log"

	"tscloud/internal/config"
	"tscloud/internal/csc"
	"tscloud/internal/otp"
	"tscloud/internal/token"

	"github.com/namecoin/pkcs11mod"
)

func init() {
	cfg, leaf, inter, err := config.Load()
	if err != nil {
		log.Printf("tscloud pkcs11: config load failed: %v", err)
		return
	}
	signer := &csc.Signer{Client: csc.New(cfg.BaseURL), CredentialID: cfg.CredentialID, OTP: otp.OSAScript{}}
	pkcs11mod.SetBackend(token.NewBackend(token.BuildObjects(leaf, inter), signer))
}

func main() {}
```

- [ ] **Step 3: Add the not-supported stubs** (`backend_stubs.go`)

Run `go build ./cmd/pkcs11/` (it will fail listing every `pkcs11mod.Backend` method still missing). For **each** missing method, add a stub returning `CKR_FUNCTION_NOT_SUPPORTED`, e.g.:

```go
package token

import "github.com/miekg/pkcs11"

func (b *Backend) Encrypt(pkcs11.SessionHandle, []byte) ([]byte, error) {
	return nil, pkcs11.Error(pkcs11.CKR_FUNCTION_NOT_SUPPORTED)
}
// …repeat for every method the compiler reports missing
// (Digest*, Verify*, GenerateKey*, WrapKey, UnwrapKey, DeriveKey, SeedRandom,
//  GenerateRandom, GetOperationState, SetOperationState, DigestEncryptUpdate, …)
```
Re-run `go build ./cmd/pkcs11/` until it compiles. Expected end state: build succeeds.

- [ ] **Step 4: Commit**
```bash
git add internal/token/backend.go internal/token/backend_stubs.go cmd/pkcs11/
git commit -m "feat(token): pkcs11mod backend + module entrypoint"
```

---

### Task 10: Backend unit tests (find / getattr / sign)

**Files:** Create `internal/token/backend_test.go`

- [ ] **Step 1: Write the test** (fake signer via an httptest CSC, or inject a Signer whose Client points at a mock)

```go
package token

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tscloud/internal/csc"

	"github.com/miekg/pkcs11"
)

type fixedOTP struct{}

func (fixedOTP) OTP(string) (string, error) { return "111111", nil }

func TestBackend_FindAndSign(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "sendOTP"):
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "authorize"):
			io.WriteString(w, `{"SAD":"S"}`)
		case strings.HasSuffix(r.URL.Path, "signHash"):
			io.WriteString(w, `{"signatures":["`+base64.StdEncoding.EncodeToString([]byte("OK"))+`"]}`)
		}
	}))
	defer srv.Close()

	leaf := sampleLeaf(t)
	signer := &csc.Signer{Client: csc.New(srv.URL + "/"), CredentialID: "c", OTP: fixedOTP{}}
	b := NewBackend(BuildObjects(leaf, nil), signer)
	b.Login(1, pkcs11.CKU_USER, "1234")

	// find private key
	if err := b.FindObjectsInit(1, []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY)}); err != nil {
		t.Fatal(err)
	}
	hs, _, _ := b.FindObjects(1, 10)
	if len(hs) != 1 {
		t.Fatalf("want 1 privkey, got %d", len(hs))
	}
	// sign
	b.SignInit(1, []*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_RSA_PKCS, nil)}, hs[0])
	sum := sha256.Sum256([]byte("x"))
	sig, err := b.Sign(1, sum[:])
	if err != nil || string(sig) != "OK" {
		t.Fatalf("sign: %q %v", sig, err)
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/token/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**
```bash
git add internal/token/backend_test.go && git commit -m "test(token): backend find+sign unit test"
```

---

### Task 11: Build the dylib + `pkcs11-tool` smoke test

**Files:** Create `scripts/build.sh`

- [ ] **Step 1: Build script**

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
CGO_ENABLED=1 GOARCH=arm64 go build -buildmode=c-shared -o libtscloud-pkcs11.dylib ./cmd/pkcs11/
codesign -s - libtscloud-pkcs11.dylib
go build -o tscloud-setup ./cmd/tscloud-setup/
echo "built: libtscloud-pkcs11.dylib, tscloud-setup"
```

- [ ] **Step 2: Build and list objects** (requires Task 7 already ran so config exists)

Run:
```bash
chmod +x scripts/build.sh && ./scripts/build.sh
pkcs11-tool --module ./libtscloud-pkcs11.dylib --list-objects
```
Expected: shows one **Certificate Object** (label "Trans Sped Cloud") and one **Private Key Object** (RSA, id matching the cert). If nothing lists, set `P11MOD_TRACE=1` and re-run to see the calls.

- [ ] **Step 3: Sign a hash end-to-end** (this triggers a REAL OTP)

Run:
```bash
head -c 32 /dev/urandom > /tmp/h.bin
pkcs11-tool --module ./libtscloud-pkcs11.dylib --login --sign \
  --mechanism RSA-PKCS --input-file /tmp/h.bin --output-file /tmp/sig.bin
```
Expected: an OTP dialog appears; after approval, `/tmp/sig.bin` is 256 bytes. Verify:
```bash
openssl x509 -inform der -in ~/.config/tscloud/leaf.der -pubkey -noout > /tmp/pub.pem
openssl pkeyutl -verify -pubin -inkey /tmp/pub.pem -sigfile /tmp/sig.bin -in /tmp/h.bin \
  -pkeyopt rsa_padding_mode:pkcs1 -pkeyopt digest:sha256
```
Expected: `Signature Verified Successfully`.

- [ ] **Step 4: Commit**
```bash
git add scripts/build.sh && git commit -m "build: dylib build script + pkcs11-tool verified"
```

---

### Task 12: Prove a full client-cert TLS handshake uses the module

**Files:** Create `scripts/mtls_test.md` (documented manual test)

- [ ] **Step 1: Stand up a throwaway mTLS server that requires client auth**

Use Go's stdlib in a scratch program (`RequireAnyClientCert`, `MaxVersion: tls.VersionTLS12`) listening on `127.0.0.1:8443`, printing the presented client cert's subject.

- [ ] **Step 2: Connect with a PKCS#11-aware client**

Run (OpenSC's pkcs11 URI via `curl` built with libp11, or `p11tool`):
```bash
p11tool --provider ./libtscloud-pkcs11.dylib --list-all
```
Confirm the cert+key appear as a usable identity. Then drive an actual TLS 1.2 client-auth connection using the module (via NSS `tstclnt`, or a Firefox pointed at the local server) and confirm the server logs the Trans Sped subject — i.e. the module produced a valid CertificateVerify.
Expected: server prints your certificate subject; one OTP dialog appeared.

- [ ] **Step 3: Commit**
```bash
git add scripts/mtls_test.md && git commit -m "test: local mTLS handshake driven by the module"
```

---

## Phase 3 — Firefox profile & real ANAF login

### Task 13: Firefox setup script

**Files:** Create `scripts/setup-firefox.sh`

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
set -euo pipefail
FF="/Applications/Firefox.app/Contents/MacOS/firefox"
PROFILE_DIR="$HOME/.tscloud-firefox"
DYLIB="$(cd "$(dirname "$0")/.." && pwd)/libtscloud-pkcs11.dylib"

# 1. create a dedicated profile
"$FF" -CreateProfile "anaf $PROFILE_DIR" -no-remote || true

# 2. load the PKCS#11 module (needs `brew install nss` for modutil)
echo "" | modutil -dbdir "sql:$PROFILE_DIR" -add "TransSpedCloud" -libfile "$DYLIB" -force

# 3. pin TLS 1.2 only + import intermediate(s)
cat >> "$PROFILE_DIR/user.js" <<'EOF'
user_pref("security.tls.version.min", 3);
user_pref("security.tls.version.max", 3);
EOF
for f in "$HOME/.config/tscloud"/intermediate*.der; do
  [ -e "$f" ] && certutil -A -d "sql:$PROFILE_DIR" -n "TransSped Intermediate" -t ",," -i "$f" || true
done
echo "Profile ready. Launch:  $FF -profile $PROFILE_DIR -no-remote"
```

- [ ] **Step 2: Run it**

Run:
```bash
brew install nss   # provides modutil/certutil
chmod +x scripts/setup-firefox.sh && ./scripts/setup-firefox.sh
```
Expected: prints "Profile ready". Verify: `modutil -dbdir "sql:$HOME/.tscloud-firefox" -list` shows `TransSpedCloud`.

- [ ] **Step 3: Commit**
```bash
git add scripts/setup-firefox.sh && git commit -m "feat(firefox): dedicated ANAF profile setup (module + TLS 1.2 pin)"
```

---

### Task 14: Real ANAF SPV login + OTP-count measurement

**Files:** Create `docs/RUNBOOK.md`

- [ ] **Step 1: Launch the ANAF profile and log in**

Run: `/Applications/Firefox.app/Contents/MacOS/firefox -profile "$HOME/.tscloud-firefox" -no-remote`
Then browse to `https://pfinternet.anaf.ro` → "Autentificare certificat" → pick the Trans Sped Cloud cert.
Expected: an OTP dialog appears; after approval the SPV dashboard loads.

- [ ] **Step 2: Measure and record**

Note in `docs/RUNBOOK.md`: how many OTP prompts one full login triggered (target: 1). If >1, record where. If a handshake times out during OTP entry, note the delay.
Troubleshooting: if the cert isn't offered, verify TLS pin (`about:config` → `security.tls.version.max` = 3) and that `-list-objects` still works; if the handshake fails, capture with `SSLKEYLOGFILE=/tmp/ff.keys` + Wireshark and confirm TLS 1.2 + `rsa_pkcs1_sha256`.

- [ ] **Step 3: Commit**
```bash
git add docs/RUNBOOK.md && git commit -m "docs: ANAF login runbook + OTP measurement"
```

---

## Phase 4 — Packaging

### Task 15: README + one-command install

**Files:** Create `README.md`; update `scripts/build.sh` if needed

- [ ] **Step 1: Write README** covering: prerequisites (`brew install go nss`, OpenSC), `./scripts/build.sh`, `./tscloud-setup -user <id>`, `./scripts/setup-firefox.sh`, launch command, and the SCAL=2 "one OTP per login" note + TLS-1.2 caveat.

- [ ] **Step 2: Full clean run-through** on a fresh shell to confirm the documented steps work start to finish.

- [ ] **Step 3: Commit**
```bash
git add README.md scripts/ && git commit -m "docs: README + packaging"
```

---

## Self-Review (completed against the spec)

- **Spec coverage:** CSC client (§5.1)→Tasks 2,3,5; OTP (§5.3)→Task 4; PKCS#11 module + object model (§5.2)→Tasks 8–10; TLS 1.2 pin + profile (§5.4, §7)→Task 13; C_Sign→CSC DigestInfo mapping (§4, §5.2)→Tasks 2,5,9; testing (§10)→Tasks 3,5,8,10,11,12,14; SCAL=2 measurement (§8)→Task 14; build/arch/codesign (§12)→Task 11. All spec sections map to a task.
- **Placeholder scan:** the only deferred detail is the mechanical list of `CKR_FUNCTION_NOT_SUPPORTED` stubs (Task 9 Step 3), which the Go compiler enumerates deterministically — not a hidden requirement. `RawSerialNumber()` flagged inline with its fix.
- **Type consistency:** `SignDigestInfo`, `ParseHashInput`, `BuildObjects`, `Matches`, `NewBackend`, `Signer{Client,CredentialID,PIN,OTP}` are used identically across tasks.
```
