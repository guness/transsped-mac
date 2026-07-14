// Command pkcs11 is the tscloud PKCS#11 module entrypoint. Built as a
// c-shared library (a later task), it registers a Backend with pkcs11mod so
// NSS/Firefox can drive the Trans Sped Cloud signing credential through the
// standard PKCS#11 API.
package main

import (
	"log"
	"os"

	"tscloud/internal/config"
	"tscloud/internal/csc"
	"tscloud/internal/otp"
	"tscloud/internal/token"

	"github.com/namecoin/pkcs11mod"
)

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
	// Headless OTP override for automated/CI testing: when TSCLOUD_OTP is set,
	// use it as a static OTP instead of popping the interactive osascript
	// dialog. Unset (the default, real-user path) is unchanged.
	var prompter otp.Prompter = otp.OSAScript{}
	if v := os.Getenv("TSCLOUD_OTP"); v != "" {
		prompter = otp.Static{Value: v}
	}
	signer := &csc.Signer{Client: csc.New(cfg.BaseURL), CredentialID: cfg.CredentialID, OTP: prompter}
	pkcs11mod.SetBackend(token.NewBackend(token.BuildObjects(leaf, inter), signer))
}

func main() {}
