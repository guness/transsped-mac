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
		log.Printf("tscloud pkcs11: config load failed: %v", err)
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
