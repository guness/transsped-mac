package otp

import (
	"fmt"
	"os/exec"
	"strings"
)

// Prompter collects the secrets needed to authorise a cloud signature: the
// signature PIN (a.k.a. signature password) and a one-time OTP. They are asked
// at signing time because ANAF's F5 APM requests the client cert via TLS
// renegotiation, during which NSS never performs a C_Login/PIN prompt.
type Prompter interface {
	// OTP prompts for just the OTP code (used when the PIN is already known,
	// e.g. remembered in the Keychain).
	OTP(prompt string) (string, error)
	// Collect prompts for the PIN and OTP together in one dialog, and reports
	// whether the user asked to remember the PIN on this Mac.
	Collect(pinPrompt, otpPrompt string) (pin, otp string, remember bool, err error)
}

// OSAScript shows native macOS dialogs and returns the typed values.
type OSAScript struct{}

func (OSAScript) OTP(prompt string) (string, error) {
	return dialog("ANAF login — Trans Sped OTP", prompt)
}

// Collect shows a single dialog with a PIN field, an OTP field, and a
// "Remember PIN" checkbox (an AppKit NSAlert accessory view, driven through
// AppleScriptObjC — `display dialog` supports only one input field). If that
// dialog can't be shown for any reason, it falls back to two sequential
// `display dialog` prompts so login still works.
func (o OSAScript) Collect(pinPrompt, otpPrompt string) (pin, otp string, remember bool, err error) {
	out, e := exec.Command("osascript", "-e", combinedDialog,
		"ANAF login — Trans Sped",
		"Enter your signature PIN and the one-time code to authorise the ANAF login.",
		pinPrompt, otpPrompt).Output()
	if e == nil {
		s := strings.TrimRight(string(out), "\n")
		if s == "CANCELLED" {
			return "", "", false, fmt.Errorf("cancelled")
		}
		if parts := strings.Split(s, "\n"); len(parts) >= 3 {
			return parts[0], parts[1], parts[2] == "1", nil
		}
	}
	// Fallback: sequential dialogs (PIN with a "Remember" button, then OTP).
	return o.collectSequential(pinPrompt, otpPrompt)
}

func (o OSAScript) collectSequential(pinPrompt, otpPrompt string) (string, string, bool, error) {
	pin, remember, err := o.pinWithRemember(pinPrompt)
	if err != nil {
		return "", "", false, err
	}
	otp, err := o.OTP(otpPrompt)
	if err != nil {
		return "", "", false, err
	}
	return pin, otp, remember, nil
}

func (OSAScript) pinWithRemember(prompt string) (string, bool, error) {
	script := `set r to display dialog "` + escape(prompt) + `" default answer "" with title "ANAF login — Trans Sped PIN" with hidden answer buttons {"Cancel","Remember & OK","OK"} default button "OK"`
	out, err := exec.Command("osascript", "-e", script,
		"-e", `(text returned of r) & linefeed & (button returned of r)`).Output()
	if err != nil {
		return "", false, err
	}
	parts := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	remember := len(parts) > 1 && strings.TrimSpace(parts[1]) == "Remember & OK"
	return parts[0], remember, nil
}

func dialog(title, prompt string) (string, error) {
	script := `display dialog "` + escape(prompt) + `" default answer "" with title "` +
		escape(title) + `" buttons {"Cancel","OK"} default button "OK"`
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

// combinedDialog is an AppleScriptObjC program (run via osascript) that shows an
// NSAlert with a two-field + checkbox accessory view. Its `on run argv` handler
// takes: title, informative text, PIN placeholder, OTP placeholder. It prints
// "pin\notp\n{0|1}" on OK, or "CANCELLED" otherwise.
const combinedDialog = `use framework "Foundation"
use framework "AppKit"
use scripting additions
on run argv
	set a to current application
	a's NSApplication's sharedApplication()
	a's NSApp's setActivationPolicy:(a's NSApplicationActivationPolicyAccessory)
	a's NSApp's activateIgnoringOtherApps:true
	set alertObj to a's NSAlert's alloc()'s init()
	alertObj's setMessageText:(item 1 of argv)
	alertObj's setInformativeText:(item 2 of argv)
	alertObj's addButtonWithTitle:"OK"
	alertObj's addButtonWithTitle:"Cancel"
	set v to a's NSView's alloc()'s initWithFrame:(a's NSMakeRect(0, 0, 320, 95))
	set pinF to a's NSSecureTextField's alloc()'s initWithFrame:(a's NSMakeRect(0, 63, 320, 24))
	pinF's setPlaceholderString:(item 3 of argv)
	set otpF to a's NSTextField's alloc()'s initWithFrame:(a's NSMakeRect(0, 33, 320, 24))
	otpF's setPlaceholderString:(item 4 of argv)
	set remB to a's NSButton's alloc()'s initWithFrame:(a's NSMakeRect(0, 4, 320, 22))
	remB's setButtonType:(a's NSButtonTypeSwitch)
	remB's setTitle:"Remember PIN on this Mac"
	v's addSubview:pinF
	v's addSubview:otpF
	v's addSubview:remB
	alertObj's setAccessoryView:v
	set r to alertObj's runModal()
	if r = (a's NSAlertFirstButtonReturn) then
		return ((pinF's stringValue()) as text) & linefeed & ((otpF's stringValue()) as text) & linefeed & ((remB's state()) as text)
	else
		return "CANCELLED"
	end if
end run`

// Static is a test/CI prompter returning fixed values.
type Static struct{ PINValue, OTPValue string }

func (s Static) OTP(string) (string, error) { return s.OTPValue, nil }
func (s Static) Collect(_, _ string) (string, string, bool, error) {
	return s.PINValue, s.OTPValue, false, nil
}
