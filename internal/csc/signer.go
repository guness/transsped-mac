package csc

import (
	"encoding/base64"
	"log"
)

// OTPPrompter collects the PIN and OTP from the user at signing time.
type OTPPrompter interface {
	OTP(prompt string) (string, error)
	Collect(pinPrompt, otpPrompt string) (pin, otp string, remember bool, err error)
}

// PINStore optionally persists a remembered signature PIN (e.g. the macOS
// Keychain). A nil store simply means "never remember".
type PINStore interface {
	Load() (string, bool)
	Save(pin string) error
	Delete() error
}

type Signer struct {
	Client       *Client
	CredentialID string
	PIN          func() string // NSS-cached PIN (OAuth C_Login path); usually nil/empty
	OTP          OTPPrompter
	Store        PINStore // optional; when set, honours the "remember PIN" checkbox
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

	// Resolve the PIN. Priority: NSS-cached (OAuth C_Login) > remembered
	// (Keychain) > prompt. When we have to prompt, ask for PIN+OTP together and
	// honour the "remember" checkbox; otherwise only the OTP is needed.
	pin := ""
	if s.PIN != nil {
		pin = s.PIN()
	}
	usedRemembered := false
	if pin == "" && s.Store != nil {
		if p, ok := s.Store.Load(); ok {
			pin, usedRemembered = p, true
		}
	}

	var otp string
	if pin != "" {
		if otp, err = s.OTP.OTP("Enter the OTP from your Trans Sped app/email to authorise the ANAF login:"); err != nil {
			return nil, err
		}
	} else {
		var remember bool
		pin, otp, remember, err = s.OTP.Collect(
			"Signature PIN (password)",
			"One-time code (OTP)")
		if err != nil {
			return nil, err
		}
		if remember && pin != "" && s.Store != nil {
			_ = s.Store.Save(pin)
		}
	}

	sad, err := s.Client.Authorize(s.CredentialID, pin, otp, hb)
	if err != nil {
		// A remembered PIN that fails to authorise is probably stale (changed or
		// mistyped when saved) — forget it so the next login prompts afresh
		// rather than failing forever.
		if usedRemembered && s.Store != nil {
			_ = s.Store.Delete()
		}
		return nil, err
	}
	sig64, err := s.Client.SignHash(s.CredentialID, sad, hb, signAlgo, hashAlgo)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(sig64)
}
