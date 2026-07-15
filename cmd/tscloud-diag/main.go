// Command tscloud-diag exercises the exact CSC signing path the PKCS#11 module
// uses (csc.Signer.SignDigestInfo), against the REAL cloud, for a chosen hash
// size — so we can validate the SHA-384 raw-signing fix without going through
// Firefox or pkcs11-tool. It prompts for the signature PIN and an OTP, signs a
// test digest, and verifies the result against the certificate's public key.
package main

import (
	"bufio"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"flag"
	"fmt"
	"os"
	"strings"

	"tscloud/internal/config"
	"tscloud/internal/csc"
)

type stdinOTP struct{ r *bufio.Reader }

func (s stdinOTP) PIN(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, "\n"+prompt+"\nPIN: ")
	line, _ := s.r.ReadString('\n')
	return strings.TrimSpace(line), nil
}

func (s stdinOTP) OTP(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, "\n"+prompt+"\nOTP: ")
	line, _ := s.r.ReadString('\n')
	return strings.TrimSpace(line), nil
}

func main() {
	bits := flag.Int("sha", 384, "hash size to test: 256, 384, or 512")
	flag.Parse()

	cfg, leaf, _, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load failed (run tscloud-setup first):", err)
		os.Exit(1)
	}
	pub, ok := leaf.PublicKey.(*rsa.PublicKey)
	if !ok {
		fmt.Fprintln(os.Stderr, "leaf key is not RSA")
		os.Exit(1)
	}

	var digest []byte
	var hh crypto.Hash
	switch *bits {
	case 256:
		d := sha256.Sum256([]byte("tscloud-diag test"))
		digest, hh = d[:], crypto.SHA256
	case 384:
		d := sha512.Sum384([]byte("tscloud-diag test"))
		digest, hh = d[:], crypto.SHA384
	case 512:
		d := sha512.Sum512([]byte("tscloud-diag test"))
		digest, hh = d[:], crypto.SHA512
	default:
		fmt.Fprintln(os.Stderr, "sha must be 256, 384, or 512")
		os.Exit(2)
	}

	r := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "Signature PIN (visible): ")
	pinLine, _ := r.ReadString('\n')
	pin := strings.TrimSpace(pinLine)

	c := csc.New(cfg.BaseURL)
	c.Debug = true
	signer := &csc.Signer{Client: c, CredentialID: cfg.CredentialID, PIN: func() string { return pin }, OTP: stdinOTP{r: r}, Debug: true}

	fmt.Fprintf(os.Stderr, "Signing a %d-byte SHA-%d digest via the module's signing path...\n", len(digest), *bits)
	sig, err := signer.SignDigestInfo(digest) // bare digest -> ParseHashInput -> (raw fix for 384)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nSIGN FAILED:", err)
		os.Exit(1)
	}
	if err := rsa.VerifyPKCS1v15(pub, hh, digest, sig); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAIL: got %d signature bytes but they do NOT verify as SHA-%d: %v\n", len(sig), *bits, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\nPASS: SHA-%d signature (%d bytes) verifies against your certificate.\n", *bits, len(sig))
}
