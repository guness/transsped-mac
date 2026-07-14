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
