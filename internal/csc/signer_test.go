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
