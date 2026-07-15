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
	// The msign cloud signs sha256WithRSA / sha512WithRSA / sha1WithRSA. The
	// working ANAF path is the F5 APM portal (app.anaf.ro), which requests
	// rsa_pkcs1_sha256 — well within that set. (The cloud rejects sha384WithRSA
	// outright, so ANAF's other, OAuth portal is unsupported here.)
	if s.Debug {
		log.Printf("[tscloud] sign: C_Sign input=%d bytes -> digest=%d bytes, signAlgo=%s hashAlgo=%s", len(data), len(digest), signAlgo, hashAlgo)
	}
	hb := base64.StdEncoding.EncodeToString(digest)
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
