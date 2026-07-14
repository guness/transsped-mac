package csc

import (
	"crypto/sha256"
	"encoding/asn1"
	"testing"
)

// digestInfoSHA256 wraps a 32-byte digest exactly as NSS does for CKM_RSA_PKCS.
func digestInfoSHA256(t *testing.T, digest []byte) []byte {
	t.Helper()
	type algID struct {
		OID  asn1.ObjectIdentifier
		Null asn1.RawValue
	}
	type di struct {
		Alg    algID
		Digest []byte
	}
	b, err := asn1.Marshal(di{
		Alg:    algID{OID: asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}, Null: asn1.RawValue{Tag: 5}},
		Digest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseHashInput_DigestInfoSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	in := digestInfoSHA256(t, sum[:])
	raw, sign, hash, err := ParseHashInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(sum[:]) {
		t.Fatalf("raw digest mismatch")
	}
	if sign != "1.2.840.113549.1.1.11" || hash != "2.16.840.1.101.3.4.2.1" {
		t.Fatalf("wrong OIDs: %s / %s", sign, hash)
	}
}

func TestParseHashInput_BareDigest(t *testing.T) {
	sum := sha256.Sum256([]byte("hi"))
	raw, sign, _, err := ParseHashInput(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 32 || sign != "1.2.840.113549.1.1.11" {
		t.Fatalf("bare 32-byte digest not handled")
	}
}
