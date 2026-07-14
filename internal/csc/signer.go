package csc

import "encoding/base64"

type OTPPrompter interface {
	OTP(prompt string) (string, error)
}

type Signer struct {
	Client       *Client
	CredentialID string
	PIN          func() string
	OTP          OTPPrompter
}

// SignDigestInfo runs the full SCAL2 flow for one signature and returns raw
// signature bytes. `data` is the DigestInfo (or bare digest) NSS handed C_Sign.
func (s *Signer) SignDigestInfo(data []byte) ([]byte, error) {
	digest, signAlgo, hashAlgo, err := ParseHashInput(data)
	if err != nil {
		return nil, err
	}
	hb := base64.StdEncoding.EncodeToString(digest)
	if err := s.Client.SendOTP(s.CredentialID); err != nil {
		return nil, err
	}
	otp, err := s.OTP.OTP("Enter the OTP from your Trans Sped app to authorise the ANAF login:")
	if err != nil {
		return nil, err
	}
	sad, err := s.Client.Authorize(s.CredentialID, s.PIN(), otp, hb)
	if err != nil {
		return nil, err
	}
	sig64, err := s.Client.SignHash(s.CredentialID, sad, hb, signAlgo, hashAlgo)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(sig64)
}
