package csc

import (
	"encoding/base64"
	"log"
)

type OTPPrompter interface {
	PIN(prompt string) (string, error)
	OTP(prompt string) (string, error)
}

type Signer struct {
	Client       *Client
	CredentialID string
	PIN          func() string
	OTP          OTPPrompter
	Debug        bool
}

// SignDigestInfo runs the full SCAL2 flow for one signature and returns raw
// signature bytes. `data` is the DigestInfo (or bare digest) NSS handed C_Sign.
func (s *Signer) SignDigestInfo(data []byte) ([]byte, error) {
	digest, signAlgo, hashAlgo, err := ParseHashInput(data)
	if err != nil {
		if s.Debug {
			log.Printf("[tscloud] sign: ParseHashInput FAILED on %d input bytes: %v", len(data), err)
		}
		return nil, err
	}
	// The msign cloud accepts sha256WithRSA / sha512WithRSA / sha1WithRSA, but
	// REJECTS sha384WithRSA ("Invalid parameter signAlgo"). ANAF's login.anaf.ro
	// handshake requests rsa_pkcs1_sha384, so for SHA-384 we build the DigestInfo
	// ourselves and sign it with the raw rsaEncryption primitive, which yields an
	// identical, valid rsa_pkcs1_sha384 signature.
	payload := digest
	if signAlgo == oidSHA384WithRSA {
		payload = append(append([]byte{}, sha384DigestInfoPrefix...), digest...)
		signAlgo = oidRSAEncryptionRaw
		hashAlgo = oidRawHashAlgo
	}
	if s.Debug {
		log.Printf("[tscloud] sign: C_Sign input=%d bytes -> digest=%d bytes, payload=%d bytes, signAlgo=%s hashAlgo=%s", len(data), len(digest), len(payload), signAlgo, hashAlgo)
	}
	hb := base64.StdEncoding.EncodeToString(payload)
	if err := s.Client.SendOTP(s.CredentialID); err != nil {
		return nil, err
	}
	// PIN: use the one NSS cached via C_Login if present (the OAuth path); on the
	// F5 APM renegotiation path NSS never logs in, so collect it here — mirroring
	// the Windows KSP's PIN+OTP dialog.
	pin := ""
	if s.PIN != nil {
		pin = s.PIN()
	}
	if pin == "" {
		pin, err = s.OTP.PIN("Enter your Trans Sped signature PIN (password):")
		if err != nil {
			return nil, err
		}
	}
	otp, err := s.OTP.OTP("Enter the OTP from your Trans Sped app/email to authorise the ANAF login:")
	if err != nil {
		return nil, err
	}
	sad, err := s.Client.Authorize(s.CredentialID, pin, otp, hb)
	if err != nil {
		return nil, err
	}
	sig64, err := s.Client.SignHash(s.CredentialID, sad, hb, signAlgo, hashAlgo)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(sig64)
}
