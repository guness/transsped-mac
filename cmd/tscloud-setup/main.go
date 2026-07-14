package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"tscloud/internal/config"
	"tscloud/internal/csc"
)

func main() {
	base := flag.String("base", "https://msign.transsped.ro/csc/v0/local/", "CSC base URL")
	user := flag.String("user", "", "Trans Sped userID (email or phone)")
	flag.Parse()
	if *user == "" {
		fmt.Fprintln(os.Stderr, "usage: tscloud-setup -user <email|phone> [-base URL]")
		os.Exit(2)
	}
	c := csc.New(*base)
	ids, err := c.List(*user)
	must(err)
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "no credentials for that user on", *base)
		os.Exit(1)
	}
	cred := ids[0]
	fmt.Println("credentialID:", cred)
	info, err := c.Info(cred)
	must(err)
	if len(info.CertB64) == 0 {
		fmt.Fprintln(os.Stderr, "credentials/info returned no certificate")
		os.Exit(1)
	}
	leaf, err := base64.StdEncoding.DecodeString(clean(info.CertB64[0]))
	must(err)
	var inter [][]byte
	for _, b := range info.CertB64[1:] {
		d, err := base64.StdEncoding.DecodeString(clean(b))
		if err == nil {
			inter = append(inter, d)
		}
	}
	must(config.Save(&config.Config{BaseURL: *base, UserID: *user, CredentialID: cred, Label: "Trans Sped Cloud"}, leaf, inter))
	fmt.Printf("Saved config + %d cert(s) to %s (SCAL=%s)\n", 1+len(inter), config.Dir(), info.SCAL)
}

func clean(s string) string { // strip PEM armor/whitespace if present
	out := ""
	for _, line := range splitLines(s) {
		if line == "" || line[0] == '-' {
			continue
		}
		out += line
	}
	return out
}
func splitLines(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
}
func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
