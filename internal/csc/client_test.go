package csc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
			// Lock the full authorize wire contract.
			if req["credentialID"] != "cred1" {
				http.Error(w, `{"error":"bad credentialID"}`, 400)
				return
			}
			if req["numSignatures"] != "1" {
				http.Error(w, `{"error":"bad numSignatures"}`, 400)
				return
			}
			if !reflect.DeepEqual(req["hash"], []any{"aGFzaA=="}) {
				http.Error(w, `{"error":"bad hash"}`, 400)
				return
			}
			if req["PIN"] != "1234" || req["OTP"] != "999000" {
				http.Error(w, `{"error":"bad pin/otp"}`, 400)
				return
			}
			io.WriteString(w, `{"SAD":"SAD-TOKEN","expiresIn":300}`)
		case strings.HasSuffix(r.URL.Path, "/signatures/signHash"):
			var req map[string]any
			json.Unmarshal(body, &req)
			// Lock the full signHash wire contract.
			if req["credentialID"] != "cred1" {
				http.Error(w, `{"error":"bad credentialID"}`, 400)
				return
			}
			if req["signAlgo"] != "1.2.840.113549.1.1.11" {
				http.Error(w, `{"error":"bad signAlgo"}`, 400)
				return
			}
			if req["hashAlgo"] != "2.16.840.1.101.3.4.2.1" {
				http.Error(w, `{"error":"bad hashAlgo"}`, 400)
				return
			}
			if req["signAlgoParams"] != "" {
				http.Error(w, `{"error":"bad signAlgoParams"}`, 400)
				return
			}
			if req["SAD"] != "SAD-TOKEN" {
				http.Error(w, `{"error":"bad SAD"}`, 400)
				return
			}
			if !reflect.DeepEqual(req["hash"], []any{"aGFzaA=="}) {
				http.Error(w, `{"error":"bad hash"}`, 400)
				return
			}
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

func TestClient_List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/credentials/list") {
			http.Error(w, "no route", 404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		if req["userID"] != "user-42" {
			http.Error(w, `{"error":"bad userID"}`, 400)
			return
		}
		io.WriteString(w, `{"credentialIDs":["a","b"]}`)
	}))
	defer srv.Close()

	c := New(srv.URL + "/")
	ids, err := c.List("user-42")
	if err != nil {
		t.Fatalf("list: err=%v", err)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("list: ids=%v", ids)
	}
}

func TestClient_SendOTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/credentials/sendOTP") {
			http.Error(w, "no route", 404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		if req["credentialID"] != "cred1" {
			http.Error(w, `{"error":"bad credentialID"}`, 400)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New(srv.URL + "/")
	if err := c.SendOTP("cred1"); err != nil {
		t.Fatalf("sendOTP: err=%v", err)
	}
}

func TestClient_Info(t *testing.T) {
	// A small but >200-char base64 blob that decodes to >=100 bytes, so it
	// survives the plausibleCert heuristic.
	certB64 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x30}, 200))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/credentials/info") {
			http.Error(w, "no route", 404)
			return
		}
		resp := map[string]any{
			"cert":         certB64,
			"certificates": []any{certB64},
			"SCAL":         "2",
			"authMode":     "explicit",
			"multisign":    3, // JSON number
			"key": map[string]any{
				"algo": "1.2.840.113549.1.1.1",
				"len":  2048,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL + "/")
	info, err := c.Info("cred1")
	if err != nil {
		t.Fatalf("info: err=%v", err)
	}
	// cert extracted from both cert and certificates fields.
	if len(info.CertB64) != 2 {
		t.Fatalf("info: CertB64 len=%d want 2 (%v)", len(info.CertB64), info.CertB64)
	}
	for i, cb := range info.CertB64 {
		if cb != certB64 {
			t.Fatalf("info: CertB64[%d]=%q", i, cb)
		}
	}
	if info.SCAL != "2" {
		t.Fatalf("info: SCAL=%q", info.SCAL)
	}
	if info.AuthMode != "explicit" {
		t.Fatalf("info: AuthMode=%q", info.AuthMode)
	}
	if info.Multisign != 3 {
		t.Fatalf("info: Multisign=%d", info.Multisign)
	}
	if info.KeyAlgo != "1.2.840.113549.1.1.1" {
		t.Fatalf("info: KeyAlgo=%q", info.KeyAlgo)
	}
	if info.KeyLen != 2048 {
		t.Fatalf("info: KeyLen=%d", info.KeyLen)
	}
}
