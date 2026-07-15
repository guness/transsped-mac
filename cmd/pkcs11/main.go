// Command pkcs11 is the tscloud PKCS#11 module entrypoint. Built as a
// c-shared library (a later task), it registers a Backend with pkcs11mod so
// NSS/Firefox can drive the Trans Sped Cloud signing credential through the
// standard PKCS#11 API.
package main

import (
	"io"
	"log"
	"log/syslog"
	"os"
	"path/filepath"

	"tscloud/internal/config"
	"tscloud/internal/csc"
	"tscloud/internal/otp"
	"tscloud/internal/token"

	"github.com/namecoin/pkcs11mod"
)

// setupDebug enables verbose logging when a sentinel file `DEBUG` exists in the
// config dir. This is deliberately NOT gated on an env var: Firefox loads and
// drives PKCS#11 modules for TLS inside a sandboxed child process that strips
// env vars and redirects stderr, so neither an env flag nor stderr logging is
// observable. Instead we redirect the standard logger to a file in the config
// dir (which the module already has read access to) and flip the package Debug
// flags. Returns true if debug was enabled.
func setupDebug() bool {
	dir := config.Dir()
	if _, err := os.Stat(filepath.Join(dir, "DEBUG")); err != nil {
		return false
	}
	// Route logs to BOTH a file and the system log (syslog). Firefox's
	// sandboxed TLS process cannot write our config dir, but sandboxes
	// generally still permit the syslog socket — so syslog is how we observe
	// signing inside Firefox (read with: log show --predicate 'eventMessage
	// CONTAINS "tscloud"' --last 10m).
	var ws []io.Writer
	if f, err := os.OpenFile(filepath.Join(dir, "pkcs11-debug.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		ws = append(ws, f)
	}
	if sw, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_USER, "tscloud-pkcs11"); err == nil {
		ws = append(ws, sw)
	}
	if len(ws) > 0 {
		log.SetOutput(io.MultiWriter(ws...))
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	token.Debug = true
	return true
}

func init() {
	cfg, leaf, inter, err := config.Load()
	if err != nil {
		// pkcs11mod.SetBackend MUST be called unconditionally: NSS/Firefox
		// dlopen()s this module and immediately drives it through the
		// Cryptoki API (C_GetSlotList, C_GetTokenInfo, ...). If no backend
		// is ever registered, those calls hit a nil backend inside
		// pkcs11mod and the host process (Firefox) crashes. On a config
		// load failure (e.g. `tscloud-setup` hasn't been run yet) we
		// register an empty backend instead: no objects, a bare signer.
		// The token still appears in NSS with no cert/key, which is safe
		// and lets the browser continue to run normally.
		log.Printf("tscloud pkcs11: config load failed: %v", err)
		pkcs11mod.SetBackend(token.NewBackend(nil, &csc.Signer{}))
		return
	}
	// Headless override for automated/CI testing: when TSCLOUD_OTP is set, use
	// static PIN/OTP instead of popping the interactive osascript dialogs. Unset
	// (the default, real-user path) is unchanged.
	var prompter otp.Prompter = otp.OSAScript{}
	if v := os.Getenv("TSCLOUD_OTP"); v != "" {
		prompter = otp.Static{OTPValue: v, PINValue: os.Getenv("TSCLOUD_PIN")}
	}
	signer := &csc.Signer{Client: csc.New(cfg.BaseURL), CredentialID: cfg.CredentialID, OTP: prompter}
	if setupDebug() {
		signer.Debug = true
		signer.Client.Debug = true
		log.Printf("[tscloud] module init (pid=%d): debug on, cred=%s base=%s", os.Getpid(), cfg.CredentialID, cfg.BaseURL)
	}
	pkcs11mod.SetBackend(token.NewBackend(token.BuildObjects(leaf, inter), signer))
}

func main() {}
