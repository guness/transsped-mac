// Command tscloud-app is the one-shot setup "app" for EasySign-mac. Run once
// (double-clicked from a .app bundle, or from a terminal), it:
//  1. asks for the Trans Sped userID and fetches the cloud certificate,
//  2. copies the PKCS#11 module to a stable location, and
//  3. registers that module into the user's DEFAULT Firefox profile's
//     pkcs11.txt — additively, touching nothing else (no dedicated profile,
//     no TLS pin, no disabling of existing certificates).
//
// After it runs, the user opens their normal Firefox, goes to ANAF, picks the
// "Trans Sped Cloud" certificate, and enters PIN+OTP. ANAF's cert hosts are
// TLS 1.2-only, so no version pin is needed.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"tscloud/internal/config"
	"tscloud/internal/csc"
)

const moduleName = "TransSpedCloud"

func main() {
	gui := len(os.Args) < 2 || os.Args[1] != "-cli" // .app double-click => GUI dialogs

	// 1. Locate the module dylib (bundled in .app Resources, or next to the binary).
	dylibSrc, err := findDylib()
	fail(gui, err)

	// 2. Copy it to a stable path so a moved/removed app can't break the module.
	stableDylib := filepath.Join(config.Dir(), "libtscloud-pkcs11.dylib")
	fail(gui, copyFile(dylibSrc, stableDylib, 0o755))

	// 3. Get the Trans Sped userID (reuse a saved one if present).
	base := "https://msign.transsped.ro/csc/v0/local/"
	userID := ""
	if cfg, _, _, e := config.Load(); e == nil && cfg.UserID != "" {
		userID = cfg.UserID
		base = cfg.BaseURL
	}
	if userID == "" {
		userID, err = prompt(gui, "Enter your Trans Sped userID (the email or phone registered for your cloud certificate):")
		fail(gui, err)
	}
	if strings.TrimSpace(userID) == "" {
		fail(gui, fmt.Errorf("no userID provided"))
	}

	// 4. Fetch the certificate and write config + certs.
	fail(gui, fetchCert(base, userID))

	// 5. Register the module into the default Firefox profile (Firefox must be closed).
	prof, err := defaultFirefoxProfile()
	fail(gui, err)
	if isFirefoxRunning() {
		fail(gui, fmt.Errorf("please QUIT Firefox first, then run this again (it must be closed to add the security module)"))
	}
	added, err := registerModule(prof, stableDylib)
	fail(gui, err)

	msg := "Setup complete."
	if !added {
		msg = "Already set up (module was already registered)."
	}
	msg += "\n\nOpen Firefox, go to your ANAF login, choose the certificate method, pick \"Trans Sped Cloud\", and enter your PIN + OTP when prompted."
	notify(gui, msg)
}

// findDylib returns the module path: $APP/Contents/Resources/… when bundled,
// else libtscloud-pkcs11.dylib next to the executable or in the CWD.
func findDylib() (string, error) {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	for _, p := range []string{
		filepath.Join(dir, "..", "Resources", "libtscloud-pkcs11.dylib"), // .app bundle
		filepath.Join(dir, "libtscloud-pkcs11.dylib"),                    // beside binary
		"libtscloud-pkcs11.dylib",                                        // CWD
	} {
		if abs, err := filepath.Abs(p); err == nil {
			if _, e := os.Stat(abs); e == nil {
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("libtscloud-pkcs11.dylib not found (build it with scripts/build.sh)")
}

func fetchCert(base, userID string) error {
	c := csc.New(base)
	ids, err := c.List(userID)
	if err != nil {
		return fmt.Errorf("looking up your credential: %w", err)
	}
	if len(ids) == 0 {
		return fmt.Errorf("no cloud credential found for %q on %s", userID, base)
	}
	info, err := c.Info(ids[0])
	if err != nil {
		return fmt.Errorf("fetching certificate: %w", err)
	}
	if len(info.CertB64) == 0 {
		return fmt.Errorf("the service returned no certificate")
	}
	leaf, err := base64.StdEncoding.DecodeString(strings.Map(dropSpace, info.CertB64[0]))
	if err != nil {
		return fmt.Errorf("decoding certificate: %w", err)
	}
	var inter [][]byte
	for _, b := range info.CertB64[1:] {
		if d, e := base64.StdEncoding.DecodeString(strings.Map(dropSpace, b)); e == nil {
			inter = append(inter, d)
		}
	}
	return config.Save(&config.Config{BaseURL: base, UserID: userID, CredentialID: ids[0], Label: "Trans Sped Cloud"}, leaf, inter)
}

func dropSpace(r rune) rune {
	if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
		return -1
	}
	return r
}

// --- Firefox profile handling -------------------------------------------------

func firefoxDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Firefox")
}

// defaultFirefoxProfile returns the absolute path of the profile Firefox
// actually launches: the [InstallXXXX] Default takes precedence (that's the
// per-install default), otherwise the [ProfileN] entry marked Default=1.
func defaultFirefoxProfile() (string, error) {
	ini := filepath.Join(firefoxDir(), "profiles.ini")
	data, err := os.ReadFile(ini)
	if err != nil {
		return "", fmt.Errorf("cannot read Firefox profiles.ini (%s): %w", ini, err)
	}
	type sect struct {
		name    map[string]string
		install bool
		profile bool
	}
	var sections []sect
	var cur *sect
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			hdr := line[1 : len(line)-1]
			sections = append(sections, sect{name: map[string]string{}, install: strings.HasPrefix(hdr, "Install"), profile: strings.HasPrefix(hdr, "Profile")})
			cur = &sections[len(sections)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur.name[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	resolve := func(pathVal, isRel string) string {
		if isRel == "0" || filepath.IsAbs(pathVal) {
			return pathVal
		}
		return filepath.Join(firefoxDir(), pathVal)
	}
	// 1. Install-specific default (Locked default the browser launches).
	for _, s := range sections {
		if s.install {
			if d := s.name["Default"]; d != "" {
				return filepath.Join(firefoxDir(), d), nil
			}
		}
	}
	// 2. Profile with Default=1.
	for _, s := range sections {
		if s.profile && s.name["Default"] == "1" {
			return resolve(s.name["Path"], s.name["IsRelative"]), nil
		}
	}
	// 3. Fallback: first profile.
	for _, s := range sections {
		if s.profile && s.name["Path"] != "" {
			return resolve(s.name["Path"], s.name["IsRelative"]), nil
		}
	}
	return "", fmt.Errorf("no Firefox profile found in %s", ini)
}

// registerModule appends our module block to the profile's pkcs11.txt if it is
// not already present. Returns whether it added a new entry.
func registerModule(profile, dylib string) (bool, error) {
	p := filepath.Join(profile, "pkcs11.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w (open Firefox once to create the profile, then quit and retry)", p, err)
	}
	if strings.Contains(string(data), "library="+dylib) || strings.Contains(string(data), "name="+moduleName) {
		return false, nil // already registered
	}
	block := "\nlibrary=" + dylib + "\nname=" + moduleName + "\n"
	out := strings.TrimRight(string(data), "\n") + "\n" + block
	if err := os.WriteFile(p, []byte(out), 0o600); err != nil {
		return false, fmt.Errorf("writing %s: %w", p, err)
	}
	return true, nil
}

func isFirefoxRunning() bool {
	out, _ := exec.Command("pgrep", "-x", "firefox").Output()
	return len(strings.TrimSpace(string(out))) > 0
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

// --- UI (osascript dialogs when GUI, stdio when -cli) -------------------------

func prompt(gui bool, message string) (string, error) {
	if !gui {
		fmt.Print(message + " ")
		var s string
		fmt.Scanln(&s)
		return strings.TrimSpace(s), nil
	}
	script := `display dialog "` + esc(message) + `" default answer "" with title "EasySign for Mac — Setup" buttons {"Cancel","OK"} default button "OK"`
	out, err := exec.Command("osascript", "-e", script, "-e", "text returned of result").Output()
	if err != nil {
		return "", fmt.Errorf("cancelled")
	}
	return strings.TrimSpace(string(out)), nil
}

func notify(gui bool, message string) {
	if !gui {
		fmt.Println(message)
		return
	}
	exec.Command("osascript", "-e", `display dialog "`+esc(message)+`" with title "EasySign for Mac — Setup" buttons {"OK"} default button "OK"`).Run()
}

func fail(gui bool, err error) {
	if err == nil {
		return
	}
	if gui {
		exec.Command("osascript", "-e", `display dialog "Setup could not complete:\n\n`+esc(err.Error())+`" with title "EasySign for Mac — Setup" with icon caution buttons {"OK"} default button "OK"`).Run()
	} else {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	os.Exit(1)
}

func esc(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return strings.ReplaceAll(s, "\n", `\n`)
}
