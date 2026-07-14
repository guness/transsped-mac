// Command cscmock is a local, throwaway stand-in for the CSC (Cloud
// Signature Consortium) API surface that tscloud's csc.Client talks to. It
// exists to prove the compiled PKCS#11 module's C_Sign path end-to-end
// (pkcs11-tool -> dlopen -> C_SignInit/C_Sign -> Backend.Sign -> csc.Signer
// -> HTTP -> signature -> back through the C ABI) without needing the real
// Trans Sped cloud or a live OTP.
//
// Given an RSA private key, it signs whatever digest signatures/signHash is
// asked to sign, the same way the real service does for signAlgo
// sha256WithRSA over a 32-byte SHA-256 digest.
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

// loadKey reads a PEM-encoded RSA private key, trying PKCS#1 then PKCS#8 —
// openssl genpkey/req produce either depending on flags/version.
func loadKey(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key (tried PKCS#1 and PKCS#8): %w", err)
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key in %s is not an RSA key", path)
	}
	return rsaKey, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	keyPath := flag.String("key", "", "path to a PEM RSA private key (PKCS#1 or PKCS#8)")
	addr := flag.String("addr", "127.0.0.1:8099", "listen address")
	flag.Parse()

	if *keyPath == "" {
		log.Fatal("cscmock: -key is required")
	}
	key, err := loadKey(*keyPath)
	if err != nil {
		log.Fatalf("cscmock: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/credentials/sendOTP", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{})
	})

	mux.HandleFunc("/credentials/authorize", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"SAD": "mock-sad"})
	})

	mux.HandleFunc("/signatures/signHash", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Hash []string `json:"hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Hash) == 0 || req.Hash[0] == "" {
			http.Error(w, "missing hash", http.StatusBadRequest)
			return
		}
		digest, err := base64.StdEncoding.DecodeString(req.Hash[0])
		if err != nil {
			http.Error(w, "bad base64 hash: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Reproduces exactly what the real cloud returns for signAlgo
		// sha256WithRSA over a 32-byte hash: PKCS#1 v1.5 over the raw digest.
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest)
		if err != nil {
			http.Error(w, "sign: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string][]string{"signatures": {base64.StdEncoding.EncodeToString(sig)}})
	})

	log.Printf("cscmock: listening on %s (key=%s)", *addr, *keyPath)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("cscmock: %v", err)
	}
}
