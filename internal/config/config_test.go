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

func TestSaveShrinkPrunesStaleIntermediates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TSCLOUD_DIR", tmp)
	leaf := selfSignedDER(t)
	der := selfSignedDER(t)
	cfg := &Config{BaseURL: "https://x/", UserID: "u", CredentialID: "c", Label: "L"}

	// Save with 2 intermediates.
	if err := Save(cfg, leaf, [][]byte{der, der}); err != nil {
		t.Fatal(err)
	}
	_, _, inter, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inter) != 2 {
		t.Fatalf("expected 2 intermediates, got %d", len(inter))
	}

	// Re-save with 1 intermediate; the stale intermediate1.der must be pruned.
	if err := Save(cfg, leaf, [][]byte{der}); err != nil {
		t.Fatal(err)
	}
	_, _, inter, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inter) != 1 {
		t.Fatalf("expected 1 intermediate after shrink, got %d", len(inter))
	}

	// Re-save with 0 intermediates; all intermediate files must be gone.
	if err := Save(cfg, leaf, nil); err != nil {
		t.Fatal(err)
	}
	_, _, inter, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inter) != 0 {
		t.Fatalf("expected 0 intermediates after shrink to nil, got %d", len(inter))
	}
	if _, err := os.Stat(filepath.Join(tmp, "intermediate0.der")); !os.IsNotExist(err) {
		t.Fatalf("expected intermediate0.der to be removed, stat err=%v", err)
	}
}
