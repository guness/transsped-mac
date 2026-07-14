package token

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"

	"github.com/miekg/pkcs11"
)

// Object is an in-memory PKCS#11 object: a certificate, private key, or
// intermediate certificate, addressable by a handle and attribute map.
type Object struct {
	Handle pkcs11.ObjectHandle
	Attrs  map[uint][]byte
}

// keyID derives the shared CKA_ID linking a certificate to its private key:
// the SHA-1 digest of the DER-encoded SubjectPublicKeyInfo.
func keyID(leaf *x509.Certificate) []byte {
	h := sha1.Sum(leaf.RawSubjectPublicKeyInfo)
	return h[:]
}

// b encodes a CK_ULONG attribute value (CKA_CLASS, CKA_KEY_TYPE,
// CKA_CERTIFICATE_TYPE, ...) as 8-byte little-endian, matching how
// pkcs11.NewAttribute encodes uint values as CK_ULONG on this
// (arm64, little-endian) platform.
func b(v uint) []byte {
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, uint64(v))
	return out
}

// BuildObjects constructs the in-memory PKCS#11 objects for a leaf
// certificate and its RSA private key (linked by CKA_ID), plus one
// certificate object per intermediate. Returns [cert, privkey, intermediate...].
func BuildObjects(leaf *x509.Certificate, inter []*x509.Certificate) []*Object {
	id := keyID(leaf)
	pub := leaf.PublicKey.(*rsa.PublicKey)
	exp := make([]byte, 8)
	binary.BigEndian.PutUint64(exp, uint64(pub.E))
	exp = bytes.TrimLeft(exp, "\x00")

	// RawSerialNumber() does not exist on this Go version's x509.Certificate;
	// DER-encode the serial number ourselves.
	serial, _ := asn1.Marshal(leaf.SerialNumber)

	cert := &Object{Handle: 1, Attrs: map[uint][]byte{
		pkcs11.CKA_CLASS:            b(pkcs11.CKO_CERTIFICATE),
		pkcs11.CKA_CERTIFICATE_TYPE: b(pkcs11.CKC_X_509),
		pkcs11.CKA_TOKEN:            {1},
		pkcs11.CKA_ID:               id,
		pkcs11.CKA_LABEL:            []byte("Trans Sped Cloud"),
		pkcs11.CKA_VALUE:            leaf.Raw,
		pkcs11.CKA_SUBJECT:          leaf.RawSubject,
		pkcs11.CKA_ISSUER:           leaf.RawIssuer,
		pkcs11.CKA_SERIAL_NUMBER:    serial,
	}}
	priv := &Object{Handle: 2, Attrs: map[uint][]byte{
		pkcs11.CKA_CLASS:           b(pkcs11.CKO_PRIVATE_KEY),
		pkcs11.CKA_KEY_TYPE:        b(pkcs11.CKK_RSA),
		pkcs11.CKA_TOKEN:           {1},
		pkcs11.CKA_PRIVATE:         {1},
		pkcs11.CKA_SIGN:            {1},
		pkcs11.CKA_ID:              id,
		pkcs11.CKA_LABEL:           []byte("Trans Sped Cloud"),
		pkcs11.CKA_MODULUS:         pub.N.Bytes(),
		pkcs11.CKA_PUBLIC_EXPONENT: exp,
	}}
	objs := []*Object{cert, priv}
	for i, c := range inter {
		objs = append(objs, &Object{Handle: pkcs11.ObjectHandle(3 + i), Attrs: map[uint][]byte{
			pkcs11.CKA_CLASS:            b(pkcs11.CKO_CERTIFICATE),
			pkcs11.CKA_CERTIFICATE_TYPE: b(pkcs11.CKC_X_509),
			pkcs11.CKA_TOKEN:            {1},
			pkcs11.CKA_VALUE:            c.Raw,
			pkcs11.CKA_SUBJECT:          c.RawSubject,
			pkcs11.CKA_ISSUER:           c.RawIssuer,
		}})
	}
	return objs
}

// Matches reports whether o satisfies every attribute in template, as used
// by C_FindObjectsInit-style filtering.
func (o *Object) Matches(template []*pkcs11.Attribute) bool {
	for _, a := range template {
		v, ok := o.Attrs[a.Type]
		if !ok || !bytes.Equal(v, a.Value) {
			return false
		}
	}
	return true
}
