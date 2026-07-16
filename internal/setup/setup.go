// Package setup implements the TransSped setup "engine": fetching the cloud
// certificate, registering/unregistering the PKCS#11 module in the user's
// default Firefox profile, and reporting status. It has no UI — the SwiftUI
// window and cmd/tscloud-engine drive it.
package setup

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tscloud/internal/config"
	"tscloud/internal/csc"
)

const moduleName = "TransSpedCloud"
const defaultBase = "https://msign.transsped.ro/csc/v0/local/"

// Status is the machine-readable state the UI renders.
type Status struct {
	Installed        bool   `json:"installed"`
	Account          string `json:"account"`
	CredentialID     string `json:"credentialID"`
	Label            string `json:"label"`
	CertNotAfter     string `json:"certNotAfter"`
	CertSubject      string `json:"certSubject"`
	ModuleRegistered bool   `json:"moduleRegistered"`
	FirefoxRunning   bool   `json:"firefoxRunning"`
	FirefoxProfile   string `json:"firefoxProfile"`
}

// CodedError carries a stable machine code so the UI can map it to a message.
type CodedError struct {
	Code    string
	Message string
}

func (e *CodedError) Error() string { return e.Message }

func coded(code, format string, a ...any) error {
	return &CodedError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// GetStatus reports the current install state. It never returns an error — a
// missing config simply yields Installed=false.
func GetStatus() *Status {
	s := &Status{FirefoxRunning: firefoxRunning()}
	if prof, err := defaultFirefoxProfile(); err == nil {
		s.FirefoxProfile = prof
		s.ModuleRegistered = moduleRegistered(prof)
	}
	cfg, leaf, _, err := config.Load()
	if err != nil {
		return s
	}
	s.Installed = true
	s.Account = cfg.UserID
	s.CredentialID = cfg.CredentialID
	s.Label = cfg.Label
	if leaf != nil {
		s.CertSubject = leaf.Subject.String()
		s.CertNotAfter = leaf.NotAfter.UTC().Format(time.RFC3339)
	}
	return s
}

func moduleRegistered(profile string) bool {
	data, err := os.ReadFile(filepath.Join(profile, "pkcs11.txt"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "name="+moduleName)
}

// firefoxRunning is a package var so tests can stub it — the real
// implementation shells out to pgrep, which must not be a hard test dependency.
var firefoxRunning = func() bool {
	out, _ := exec.Command("pgrep", "-x", "firefox").Output()
	return len(strings.TrimSpace(string(out))) > 0
}

// firefoxDir honors TSCLOUD_FIREFOX_DIR (a test hook so tests never touch the
// real Firefox profile); otherwise the standard macOS Firefox support dir.
func firefoxDir() string {
	if d := os.Getenv("TSCLOUD_FIREFOX_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Firefox")
}

// defaultFirefoxProfile returns the absolute path of the profile Firefox
// launches: an [Install*] Default takes precedence, else a [Profile*]
// Default=1, else the first profile.
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
	for _, s := range sections {
		if s.install {
			if d := s.name["Default"]; d != "" {
				return filepath.Join(firefoxDir(), d), nil
			}
		}
	}
	for _, s := range sections {
		if s.profile && s.name["Default"] == "1" {
			return resolve(s.name["Path"], s.name["IsRelative"]), nil
		}
	}
	for _, s := range sections {
		if s.profile && s.name["Path"] != "" {
			return resolve(s.name["Path"], s.name["IsRelative"]), nil
		}
	}
	return "", fmt.Errorf("no Firefox profile found in %s", ini)
}

// registerModule appends our module block to pkcs11.txt if not present.
func registerModule(profile, dylib string) (bool, error) {
	p := filepath.Join(profile, "pkcs11.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w (open Firefox once to create the profile, then quit and retry)", p, err)
	}
	if strings.Contains(string(data), "library="+dylib) || strings.Contains(string(data), "name="+moduleName) {
		return false, nil
	}
	block := "\nlibrary=" + dylib + "\nname=" + moduleName + "\n"
	out := strings.TrimRight(string(data), "\n") + "\n" + block
	if err := os.WriteFile(p, []byte(out), 0o600); err != nil {
		return false, fmt.Errorf("writing %s: %w", p, err)
	}
	return true, nil
}

// unregisterModule drops any pkcs11.txt record naming our module (by name or by
// the library path under ~/.config/tscloud), tolerating NSS's canonical form.
func unregisterModule(profile string) (bool, error) {
	p := filepath.Join(profile, "pkcs11.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", p, err)
	}
	stableDylib := filepath.Join(config.Dir(), "libtscloud-pkcs11.dylib")
	var records [][]string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			records = append(records, cur)
			cur = nil
		}
	}
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) == "" {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	removed := false
	var kept []string
	for _, rec := range records {
		ours := false
		for _, ln := range rec {
			switch strings.TrimSpace(ln) {
			case "name=" + moduleName, "library=" + stableDylib:
				ours = true
			}
		}
		if ours {
			removed = true
			continue
		}
		kept = append(kept, strings.Join(rec, "\n"))
	}
	if !removed {
		return false, nil
	}
	return true, os.WriteFile(p, []byte(strings.Join(kept, "\n\n")+"\n"), 0o600)
}

// findDylib returns the module path: $APP/Contents/Resources/… when bundled,
// else libtscloud-pkcs11.dylib next to the executable or in the CWD.
func findDylib() (string, error) {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	for _, p := range []string{
		filepath.Join(dir, "..", "Resources", "libtscloud-pkcs11.dylib"),
		filepath.Join(dir, "libtscloud-pkcs11.dylib"),
		"libtscloud-pkcs11.dylib",
	} {
		if abs, err := filepath.Abs(p); err == nil {
			if _, e := os.Stat(abs); e == nil {
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("libtscloud-pkcs11.dylib not found (build it with scripts/build.sh)")
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

func dropSpace(r rune) rune {
	if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
		return -1
	}
	return r
}

// fetchCert downloads the leaf + chain for userID and saves config + certs.
func fetchCert(base, userID string) error {
	c := csc.New(base)
	ids, err := c.List(userID)
	if err != nil {
		return coded("network", "looking up your credential: %v", err)
	}
	if len(ids) == 0 {
		return coded("no_credential", "no cloud credential found for %q", userID)
	}
	info, err := c.Info(ids[0])
	if err != nil {
		return coded("network", "fetching certificate: %v", err)
	}
	if len(info.CertB64) == 0 {
		return coded("no_credential", "the service returned no certificate")
	}
	leaf, err := base64.StdEncoding.DecodeString(strings.Map(dropSpace, info.CertB64[0]))
	if err != nil {
		return coded("unknown", "decoding certificate: %v", err)
	}
	var inter [][]byte
	for _, b := range info.CertB64[1:] {
		if d, e := base64.StdEncoding.DecodeString(strings.Map(dropSpace, b)); e == nil {
			inter = append(inter, d)
		}
	}
	return config.Save(&config.Config{BaseURL: base, UserID: userID, CredentialID: ids[0], Label: "Trans Sped Cloud"}, leaf, inter)
}
