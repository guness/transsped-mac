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
