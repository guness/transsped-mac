package token

import (
	"crypto/ecdsa"
	"crypto/elliptic"
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

// TestBuildObjects_NonRSANoPanic guards against a host crash: BuildObjects
// runs during the module's dlopen-time init(), so a non-RSA leaf (e.g. an
// ECDSA credential) must never panic. It should instead degrade gracefully
// to certificate-only objects with no private-key object.
func TestBuildObjects_NonRSANoPanic(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "EC Test User"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}
	ecCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("BuildObjects panicked on non-RSA leaf: %v", r)
		}
	}()

	objs := BuildObjects(ecCert, nil)

	tmplPriv := []*pkcs11.Attribute{pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY)}
	for _, o := range objs {
		if o.Matches(tmplPriv) {
			t.Fatal("BuildObjects returned a private-key object for a non-RSA leaf")
		}
	}
	if len(objs) == 0 {
		t.Fatal("BuildObjects returned no objects for a non-RSA leaf; expected at least the cert object")
	}
}
