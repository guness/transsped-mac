package otp

import (
	"os/exec"
	"strings"
)

type Prompter interface {
	OTP(prompt string) (string, error)
}

// OSAScript shows a native macOS dialog and returns the typed code.
type OSAScript struct{}

func (OSAScript) OTP(prompt string) (string, error) {
	script := `display dialog "` + escape(prompt) +
		`" default answer "" with title "ANAF login — Trans Sped OTP" with hidden answer ` +
		`buttons {"Cancel","OK"} default button "OK"`
	out, err := exec.Command("osascript", "-e", script, "-e",
		`text returned of result`).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// Static is a test/CI prompter.
type Static struct{ Value string }

func (s Static) OTP(string) (string, error) { return s.Value, nil }
