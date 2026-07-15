package otp

import (
	"os/exec"
	"strings"
)

// Prompter collects the two secrets needed to authorise a cloud signature: the
// signature PIN (a.k.a. signature password) and a one-time OTP. They are asked
// at signing time because ANAF's F5 APM requests the client cert via TLS
// renegotiation, during which NSS never performs a C_Login/PIN prompt.
type Prompter interface {
	PIN(prompt string) (string, error)
	OTP(prompt string) (string, error)
}

// OSAScript shows native macOS dialogs and returns the typed values.
type OSAScript struct{}

func (OSAScript) PIN(prompt string) (string, error) {
	return dialog("ANAF login — Trans Sped PIN", prompt)
}
func (OSAScript) OTP(prompt string) (string, error) {
	return dialog("ANAF login — Trans Sped OTP", prompt)
}

func dialog(title, prompt string) (string, error) {
	script := `display dialog "` + escape(prompt) + `" default answer "" with title "` +
		escape(title) + `" with hidden answer buttons {"Cancel","OK"} default button "OK"`
	out, err := exec.Command("osascript", "-e", script, "-e", `text returned of result`).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// Static is a test/CI prompter returning fixed values.
type Static struct{ PINValue, OTPValue string }

func (s Static) PIN(string) (string, error) { return s.PINValue, nil }
func (s Static) OTP(string) (string, error) { return s.OTPValue, nil }
