package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func selfSignedDER(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "tscloud-test",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

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
