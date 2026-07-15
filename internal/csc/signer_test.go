package csc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeOTP struct{}

func (fakeOTP) OTP(string) (string, error) { return "999000", nil }
func (fakeOTP) Collect(string, string) (string, string, bool, error) {
	return "1234", "999000", false, nil
}

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

// --- remember-PIN flow --------------------------------------------------------

type memStore struct {
	pin            string
	has            bool
	saved, deleted bool
}

func (m *memStore) Load() (string, bool) { return m.pin, m.has }
func (m *memStore) Save(p string) error  { m.pin, m.has, m.saved = p, true, true; return nil }
func (m *memStore) Delete() error        { m.pin, m.has, m.deleted = "", false, true; return nil }

type recordPrompter struct {
	otpCalled, collectCalled bool
	pin, otp                 string
	remember                 bool
}

func (r *recordPrompter) OTP(string) (string, error) { r.otpCalled = true; return r.otp, nil }
func (r *recordPrompter) Collect(string, string) (string, string, bool, error) {
	r.collectCalled = true
	return r.pin, r.otp, r.remember, nil
}

// cscServer serves sendOTP/authorize/signHash, records the PIN authorize
// received (when gotPIN != nil), and fails authorize when failAuth is set.
func cscServer(t *testing.T, failAuth bool, gotPIN *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/credentials/sendOTP"):
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/credentials/authorize"):
			if gotPIN != nil {
				var body struct {
					PIN string `json:"PIN"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				*gotPIN = body.PIN
			}
			if failAuth {
				w.WriteHeader(http.StatusInternalServerError)
				io.WriteString(w, `bad pin`)
				return
			}
			io.WriteString(w, `{"SAD":"SAD1"}`)
		case strings.HasSuffix(r.URL.Path, "/signatures/signHash"):
			io.WriteString(w, `{"signatures":["`+base64.StdEncoding.EncodeToString([]byte("SIG"))+`"]}`)
		}
	}))
}

func mustSign(t *testing.T, s *Signer) ([]byte, error) {
	t.Helper()
	sum := sha256.Sum256([]byte("x"))
	return s.SignDigestInfo(sum[:])
}

// A remembered PIN skips the PIN prompt: only the OTP is asked, and authorize
// receives the stored PIN.
func TestSigner_UsesRememberedPIN(t *testing.T) {
	var gotPIN string
	srv := cscServer(t, false, &gotPIN)
	defer srv.Close()
	store := &memStore{pin: "9999", has: true}
	pr := &recordPrompter{otp: "123456"}
	s := &Signer{Client: New(srv.URL + "/"), CredentialID: "c", OTP: pr, Store: store}
	if _, err := mustSign(t, s); err != nil {
		t.Fatal(err)
	}
	if pr.collectCalled {
		t.Error("Collect must not be called when a PIN is remembered")
	}
	if !pr.otpCalled {
		t.Error("the OTP-only prompt must be used when a PIN is remembered")
	}
	if gotPIN != "9999" {
		t.Errorf("authorize used PIN %q, want remembered 9999", gotPIN)
	}
}

// Checking "remember" saves the entered PIN to the store.
func TestSigner_RemembersPIN(t *testing.T) {
	srv := cscServer(t, false, nil)
	defer srv.Close()
	store := &memStore{}
	pr := &recordPrompter{pin: "4321", otp: "123456", remember: true}
	s := &Signer{Client: New(srv.URL + "/"), CredentialID: "c", OTP: pr, Store: store}
	if _, err := mustSign(t, s); err != nil {
		t.Fatal(err)
	}
	if !pr.collectCalled {
		t.Error("Collect must be used when no PIN is remembered")
	}
	if !store.saved || store.pin != "4321" {
		t.Errorf("PIN not saved: saved=%v pin=%q", store.saved, store.pin)
	}
}

// Leaving "remember" unchecked stores nothing.
func TestSigner_DoesNotRememberWhenUnchecked(t *testing.T) {
	srv := cscServer(t, false, nil)
	defer srv.Close()
	store := &memStore{}
	pr := &recordPrompter{pin: "4321", otp: "123456", remember: false}
	s := &Signer{Client: New(srv.URL + "/"), CredentialID: "c", OTP: pr, Store: store}
	if _, err := mustSign(t, s); err != nil {
		t.Fatal(err)
	}
	if store.saved {
		t.Error("PIN must not be saved when remember is unchecked")
	}
}

// A remembered PIN that fails authorize is forgotten so the next login prompts.
func TestSigner_ForgetsBadRememberedPIN(t *testing.T) {
	srv := cscServer(t, true, nil)
	defer srv.Close()
	store := &memStore{pin: "9999", has: true}
	pr := &recordPrompter{otp: "123456"}
	s := &Signer{Client: New(srv.URL + "/"), CredentialID: "c", OTP: pr, Store: store}
	if _, err := mustSign(t, s); err == nil {
		t.Fatal("expected authorize failure")
	}
	if !store.deleted || store.has {
		t.Errorf("bad remembered PIN must be deleted: deleted=%v has=%v", store.deleted, store.has)
	}
}
