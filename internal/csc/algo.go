package csc

import (
	"encoding/asn1"
	"fmt"
)

// OIDs used by the raw-signing fallback (see signer.go). The Trans Sped msign
// backend rejects sha384WithRSA, so SHA-384 is signed via the raw rsaEncryption
// primitive over a locally-built DigestInfo.
const (
	oidSHA384WithRSA    = "1.2.840.113549.1.1.12"
	oidRSAEncryptionRaw = "1.2.840.113549.1.1.1"
	oidRawHashAlgo      = "1.3.6.1.4.1.2706.2.4.1.1" // vendor OID the DLL pairs with rsaEncryption
)

// sha384DigestInfoPrefix is the fixed ASN.1 DigestInfo header for SHA-384
// (RFC 8017 / PKCS#1). Prepended to a 48-byte SHA-384 digest it forms the exact
// bytes an RSA PKCS#1 v1.5 signature must be computed over.
var sha384DigestInfoPrefix = []byte{
	0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01,
	0x65, 0x03, 0x04, 0x02, 0x02, 0x05, 0x00, 0x04, 0x30,
}

type oidPair struct{ sign, hash string }

// byLen maps a raw digest length to (signAlgo, hashAlgo) OIDs — the exact
// pairs the Trans Sped msign backend selects on.
var byLen = map[int]oidPair{
	20: {"1.3.14.3.2.29", "1.3.14.3.2.26"},
	32: {"1.2.840.113549.1.1.11", "2.16.840.1.101.3.4.2.1"},
	48: {"1.2.840.113549.1.1.12", "2.16.840.1.101.3.4.2.2"},
	64: {"1.2.840.113549.1.1.13", "2.16.840.1.101.3.4.2.3"},
}

type digestInfo struct {
	Alg struct {
		OID  asn1.ObjectIdentifier
		Null asn1.RawValue `asn1:"optional"`
	}
	Digest []byte
}

// ParseHashInput accepts either a DER DigestInfo (NSS CKM_RSA_PKCS input) or a
// bare digest, and returns the raw digest plus the CSC signAlgo/hashAlgo OIDs.
func ParseHashInput(data []byte) (raw []byte, signAlgo, hashAlgo string, err error) {
	var di digestInfo
	if rest, e := asn1.Unmarshal(data, &di); e == nil && len(rest) == 0 && len(di.Digest) > 0 {
		if p, ok := byLen[len(di.Digest)]; ok {
			return di.Digest, p.sign, p.hash, nil
		}
	}
	if p, ok := byLen[len(data)]; ok { // fall back: treat as a bare digest
		return data, p.sign, p.hash, nil
	}
	return nil, "", "", fmt.Errorf("unrecognized hash input of %d bytes", len(data))
}
