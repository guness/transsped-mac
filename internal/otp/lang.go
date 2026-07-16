package otp

import (
	"os/exec"
	"strings"
)

type lang int

const (
	langEN lang = iota
	langRO
)

// detectLang chooses the sign-time dialog language. It honors the app's stored
// preference (UserDefaults domain ro.transsped.macos, key appLanguage —
// "ro"/"en"/"system"), which the window's language menu writes, so the selector
// governs this dialog too. "system"/absent falls back to the Mac's preferred UI
// language. Any failure defaults to English.
func detectLang() lang {
	switch strings.TrimSpace(readDefault("ro.transsped.macos", "appLanguage")) {
	case "ro":
		return langRO
	case "en":
		return langEN
	}
	if strings.HasPrefix(strings.ToLower(firstAppleLanguage()), "ro") {
		return langRO
	}
	return langEN
}

func readDefault(domain, key string) string {
	out, err := exec.Command("defaults", "read", domain, key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// firstAppleLanguage returns the user's top preferred UI language, e.g. "en-US"
// (parsed from the first quoted entry of the AppleLanguages plist array).
func firstAppleLanguage() string {
	out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return ""
	}
	s := string(out)
	i := strings.Index(s, `"`)
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// dialogStrings is the localized copy for the PIN/OTP dialogs.
type dialogStrings struct {
	title, info, pinField, otpField, remember, ok, cancel, rememberOK string
	pinTitle, pinMessage, otpTitle, otpMessage                        string
}

func dialogCopy(l lang) dialogStrings {
	if l == langRO {
		return dialogStrings{
			title:      "Autentificare ANAF — Trans Sped",
			info:       "Introdu PIN-ul de semnătură și codul unic pentru a autoriza autentificarea ANAF.",
			pinField:   "PIN de semnătură (parolă)",
			otpField:   "Cod unic (OTP)",
			remember:   "Reține PIN-ul pe acest Mac",
			ok:         "OK",
			cancel:     "Anulează",
			rememberOK: "Reține și OK",
			pinTitle:   "Autentificare ANAF — PIN Trans Sped",
			pinMessage: "Introdu PIN-ul de semnătură Trans Sped (parola):",
			otpTitle:   "Autentificare ANAF — OTP Trans Sped",
			otpMessage: "Introdu OTP-ul din aplicația sau emailul Trans Sped pentru a autoriza autentificarea ANAF:",
		}
	}
	return dialogStrings{
		title:      "ANAF login — Trans Sped",
		info:       "Enter your signature PIN and the one-time code to authorise the ANAF login.",
		pinField:   "Signature PIN (password)",
		otpField:   "One-time code (OTP)",
		remember:   "Remember PIN on this Mac",
		ok:         "OK",
		cancel:     "Cancel",
		rememberOK: "Remember & OK",
		pinTitle:   "ANAF login — Trans Sped PIN",
		pinMessage: "Enter your Trans Sped signature PIN (password):",
		otpTitle:   "ANAF login — Trans Sped OTP",
		otpMessage: "Enter the OTP from your Trans Sped app or email to authorise the ANAF login:",
	}
}
