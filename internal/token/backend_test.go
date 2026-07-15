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

// TestFindObjects_NegativeMaxNoPanic guards against a regression where a
// PKCS#11 caller passes an unsigned "unlimited" max that casts to a negative
// Go int, which previously panicked on the b.find[:n] slice. A negative max
// now means "return all remaining".
func TestFindObjects_NegativeMaxNoPanic(t *testing.T) {
	b := NewBackend(BuildObjects(sampleLeaf(t), nil), &csc.Signer{})

	if err := b.FindObjectsInit(1, nil); err != nil {
		t.Fatalf("FindObjectsInit: %v", err)
	}
	hs, _, err := b.FindObjects(1, -1)
	if err != nil {
		t.Fatalf("FindObjects(-1): %v", err)
	}
	if len(hs) != 2 {
		t.Fatalf("want 2 handles (cert+privkey), got %d", len(hs))
	}

	if err := b.FindObjectsInit(1, nil); err != nil {
		t.Fatalf("FindObjectsInit (re-init): %v", err)
	}
	hs, _, err = b.FindObjects(1, 1)
	if err != nil {
		t.Fatalf("FindObjects(1): %v", err)
	}
	if len(hs) != 1 {
		t.Fatalf("want 1 handle (respecting positive max), got %d", len(hs))
	}
}

// TestNewBackend_EmptyNoPanic guards the cmd/pkcs11 entrypoint's
// config-load-failure fallback: NewBackend(nil, &csc.Signer{}) must produce
// a usable, empty backend that a Cryptoki host can drive (FindObjectsInit /
// FindObjects) without panicking, so a missing tscloud config never crashes
// the browser hosting the module.
func TestNewBackend_EmptyNoPanic(t *testing.T) {
	b := NewBackend(nil, &csc.Signer{})

	if err := b.FindObjectsInit(1, nil); err != nil {
		t.Fatalf("FindObjectsInit: %v", err)
	}
	hs, _, err := b.FindObjects(1, 100)
	if err != nil {
		t.Fatalf("FindObjects: %v", err)
	}
	if len(hs) != 0 {
		t.Fatalf("want 0 handles on empty backend, got %d", len(hs))
	}
}

type fixedOTP struct{}

func (fixedOTP) OTP(string) (string, error) { return "111111", nil }
func (fixedOTP) Collect(string, string) (string, string, bool, error) {
	return "1234", "111111", false, nil
}

// TestBackend_FindAndSign drives the Backend through the full
// find-private-key -> sign flow, with the CSC HTTP calls (sendOTP, authorize,
// signHash) served by an httptest server and OTP prompting satisfied by
// fixedOTP. It asserts the decoded signature bytes returned by Sign match
// what the mock server returned.
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
