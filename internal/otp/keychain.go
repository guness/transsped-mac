package otp

import (
	"os/exec"
	"strings"
)

// Keychain stores the remembered signature PIN in the macOS login keychain via
// the `security` CLI. Delegating to `security` (rather than writing a file)
// matters for two reasons: the read/write runs in the `security` child process,
// which sidesteps the Firefox TLS-process sandbox that blocks writes to
// ~/.config/tscloud; and the value is encrypted at rest in the keychain.
//
// A zero Keychain (empty Service/Account) is a no-op store — Load reports "not
// found" and Save/Delete succeed silently — so callers can leave it unset.
type Keychain struct {
	Service string
	Account string
}

func (k Keychain) ok() bool { return k.Service != "" && k.Account != "" }

// Load returns the stored PIN and whether one was found.
func (k Keychain) Load() (string, bool) {
	if !k.ok() {
		return "", false
	}
	out, err := exec.Command("security", "find-generic-password",
		"-s", k.Service, "-a", k.Account, "-w").Output()
	if err != nil {
		return "", false
	}
	pin := strings.TrimRight(string(out), "\n")
	return pin, pin != ""
}

// Save stores (or replaces) the PIN. -U updates an existing item; -A allows
// access without a per-process keychain prompt (the item is still protected by
// the login keychain being unlocked).
func (k Keychain) Save(pin string) error {
	if !k.ok() || pin == "" {
		return nil
	}
	return exec.Command("security", "add-generic-password",
		"-U", "-A", "-s", k.Service, "-a", k.Account,
		"-l", "Trans Sped Cloud PIN", "-w", pin).Run()
}

// Delete removes the stored PIN (best effort; missing item is not an error).
func (k Keychain) Delete() error {
	if !k.ok() {
		return nil
	}
	_ = exec.Command("security", "delete-generic-password",
		"-s", k.Service, "-a", k.Account).Run()
	return nil
}
