# TransSped Native App Window Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the one-shot osascript-dialog `TransSped.app` with a small native SwiftUI window (status + Set up/Update, Open ANAF, Uninstall, About), driven by the existing Go logic running headlessly as an engine.

**Architecture:** A SwiftUI app (`Contents/MacOS/TransSped`) shells out to a headless Go engine (`Contents/Resources/tscloud-engine`, subcommands emit JSON) that does cert fetch, Firefox `pkcs11.txt` registration, Keychain, and status. The PKCS#11 dylib is unchanged. All non-UI logic lives in a new tested `internal/setup` Go package; the engine `main` is a thin JSON CLI over it.

**Tech Stack:** Go 1.22 (engine + dylib), SwiftUI/AppKit compiled with `swiftc` (no Xcode project), Developer ID codesign + notarytool (unchanged).

## Global Constraints

- Go floor `go 1.22`; **no new Go dependencies** (stdlib + existing `internal/*`, `github.com/miekg/pkcs11`, vendored `pkcs11mod` only).
- macOS deployment target **13.0** (raised from 11.0 — required for the SwiftUI APIs used); set `LSMinimumSystemVersion` = `13.0` and `swiftc -target arm64-apple-macos13`.
- The PKCS#11 dylib (`cmd/pkcs11`, `internal/token`, `internal/otp`, `internal/csc` signing path) and the PIN/OTP sign-time flow are **unchanged**.
- Module name is `TransSpedCloud`; config dir is `~/.config/tscloud` (via `config.Dir()`); Keychain service is `config.KeychainService`.
- Bundle id `ro.transsped.macos`; ANAF login URL `https://pfinternet.anaf.ro`; GitHub `github.com/guness/transsped-mac`.
- Build stays **script-based** (`scripts/build-app.sh`); signing is inner-out; `scripts/make-dmg.sh` (sign + notarize + staple) is unchanged.
- Engine emits exactly one JSON object per invocation. `status` prints a bare `Status`; `setup`/`uninstall` print a result envelope. Error `code` values: `firefox_running`, `no_credential`, `no_profile`, `network`, `unknown`.

## File Structure

- Create `internal/setup/setup.go` — engine logic: `Status`, `GetStatus`, `Run`, `Uninstall`, `CodedError`, and the Firefox/dylib/cert helpers (moved from `cmd/tscloud-app`).
- Create `internal/setup/setup_test.go` — moved register/unregister round-trip tests + status/run/uninstall tests.
- Create `cmd/tscloud-engine/main.go` — thin JSON CLI (`run(args, out)` + subcommands).
- Create `cmd/tscloud-engine/main_test.go` — CLI routing + JSON shape tests.
- Delete `cmd/tscloud-app/main.go` and `cmd/tscloud-app/main_test.go` (superseded).
- Create `app/Engine.swift` — Codable models + `Process` runner + `appVersion()`.
- Create `app/AboutView.swift`, `app/ContentView.swift`, `app/TransSpedApp.swift` — the window.
- Modify `scripts/build-app.sh` — swiftc app + go engine + dylib, Info.plist, inner-out sign.
- Modify `README.md`, `docs/RUNBOOK.md`, `scripts/smoke-test.md` — window-based install steps.

---

### Task 1: `internal/setup` — status + Firefox/cert helpers

**Files:**
- Create: `internal/setup/setup.go`
- Test: `internal/setup/setup_test.go`

**Interfaces:**
- Consumes: `internal/config` (`Load`, `Save`, `Dir`, `KeychainService`, `Config`), `internal/csc` (`New`, `List`, `Info`).
- Produces: `setup.Status` struct (JSON-tagged), `setup.GetStatus() *Status`, `setup.CodedError{Code,Message}`, and package-private helpers `defaultFirefoxProfile`, `registerModule`, `unregisterModule`, `moduleRegistered`, `firefoxRunning`, `findDylib`, `copyFile`, `dropSpace`, `fetchCert`, `coded`. (`Run`/`Uninstall` are added in Task 2.)

- [ ] **Step 1: Write the failing test** — `internal/setup/setup_test.go`

```go
package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tscloud/internal/config"
)

// seedProfile writes a minimal NSS-style pkcs11.txt into a temp dir standing in
// for a Firefox profile, and returns the dir.
func seedProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	seed := "library=\nname=NSS Internal PKCS #11 Module\nparameters=configdir='sql:.'\nNSS=Flags=internal\n"
	if err := os.WriteFile(filepath.Join(dir, "pkcs11.txt"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stubFirefox replaces the firefoxRunning probe for the duration of a test so
// tests never depend on (or wait on) a real Firefox process.
func stubFirefox(t *testing.T, running bool) {
	t.Helper()
	orig := firefoxRunning
	firefoxRunning = func() bool { return running }
	t.Cleanup(func() { firefoxRunning = orig })
}

func TestRegisterUnregisterRoundTrip(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())
	prof := seedProfile(t)
	dylib := filepath.Join(config.Dir(), "libtscloud-pkcs11.dylib")

	added, err := registerModule(prof, dylib)
	if err != nil || !added {
		t.Fatalf("registerModule: added=%v err=%v", added, err)
	}
	if added2, _ := registerModule(prof, dylib); added2 {
		t.Fatal("registerModule must be idempotent")
	}
	if !moduleRegistered(prof) {
		t.Fatal("moduleRegistered should report true after register")
	}
	removed, err := unregisterModule(prof)
	if err != nil || !removed {
		t.Fatalf("unregisterModule: removed=%v err=%v", removed, err)
	}
	if moduleRegistered(prof) {
		t.Fatal("moduleRegistered should report false after unregister")
	}
	if data, _ := os.ReadFile(filepath.Join(prof, "pkcs11.txt")); !strings.Contains(string(data), "NSS Internal PKCS #11 Module") {
		t.Fatalf("unregister clobbered the internal record:\n%s", data)
	}
}

func TestGetStatus_NoConfig(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())          // empty -> no config.json
	t.Setenv("TSCLOUD_FIREFOX_DIR", t.TempDir())  // empty -> no profiles.ini
	stubFirefox(t, false)
	s := GetStatus()
	if s.Installed {
		t.Fatal("Installed must be false with no config")
	}
	if s.Account != "" || s.CertNotAfter != "" {
		t.Fatalf("expected empty account/cert, got %+v", s)
	}
	if s.ModuleRegistered {
		t.Fatal("ModuleRegistered must be false with no profile")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ 2>&1 | head`
Expected: FAIL — package/build error (`setup.go` does not exist yet).

- [ ] **Step 3: Write minimal implementation** — `internal/setup/setup.go`

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -v`
Expected: PASS — `TestRegisterUnregisterRoundTrip`, `TestGetStatus_NoConfig`.

- [ ] **Step 5: Commit**

```bash
git add internal/setup/setup.go internal/setup/setup_test.go
git commit -m "feat(setup): internal/setup package — status + Firefox/cert helpers"
```

---

### Task 2: `internal/setup` — Run + Uninstall orchestration

**Files:**
- Modify: `internal/setup/setup.go` (append `Run`, `Uninstall`)
- Test: `internal/setup/setup_test.go` (append)

**Interfaces:**
- Consumes: everything from Task 1.
- Produces: `setup.Run(userID string) (*Status, error)`, `setup.Uninstall() ([]string, error)`. Both return a `*CodedError` on the failure paths (`no_credential`, `firefox_running`, `no_profile`, `network`, `unknown`).

- [ ] **Step 1: Write the failing test** — append to `internal/setup/setup_test.go`

```go
import "errors" // add to the existing import block

// stubKeychain replaces the Keychain delete so tests never touch the real login
// Keychain.
func stubKeychain(t *testing.T) {
	t.Helper()
	orig := deleteKeychainPIN
	deleteKeychainPIN = func() bool { return false }
	t.Cleanup(func() { deleteKeychainPIN = orig })
}

func TestRun_EmptyUser(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())
	_, err := Run("   ")
	var ce *CodedError
	if !errors.As(err, &ce) || ce.Code != "no_credential" {
		t.Fatalf("want no_credential CodedError, got %v", err)
	}
}

func TestRun_FirefoxRunningIsPreflight(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("TSCLOUD_DIR", cfgDir)
	t.Setenv("TSCLOUD_FIREFOX_DIR", t.TempDir())
	stubFirefox(t, true) // Firefox "open"
	_, err := Run("+123")
	var ce *CodedError
	if !errors.As(err, &ce) || ce.Code != "firefox_running" {
		t.Fatalf("want firefox_running, got %v", err)
	}
	// No mutation must have happened (no dylib copied into the config dir).
	if _, e := os.Stat(filepath.Join(cfgDir, "libtscloud-pkcs11.dylib")); !os.IsNotExist(e) {
		t.Fatal("Run mutated state before the firefox_running pre-flight check")
	}
}

func TestUninstall_ClearsConfig(t *testing.T) {
	cfgDir := t.TempDir()
	ffDir := t.TempDir()
	t.Setenv("TSCLOUD_DIR", cfgDir)
	t.Setenv("TSCLOUD_FIREFOX_DIR", ffDir)
	stubFirefox(t, false)
	stubKeychain(t)

	// seed a config file and a Firefox profile with our module registered
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	prof := filepath.Join(ffDir, "Profiles", "test.default")
	if err := os.MkdirAll(prof, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ffDir, "profiles.ini"),
		[]byte("[Profile0]\nPath=Profiles/test.default\nIsRelative=1\nDefault=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prof, "pkcs11.txt"),
		[]byte("library=\nname=NSS Internal PKCS #11 Module\n\nlibrary=/x/libtscloud-pkcs11.dylib\nname=TransSpedCloud\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Fatalf("config dir should be gone, stat err=%v", err)
	}
	if moduleRegistered(prof) {
		t.Fatal("module should be unregistered from the profile")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/setup/ -run 'TestRun_EmptyUser|TestUninstall' 2>&1 | head`
Expected: FAIL — `Run`/`Uninstall` undefined.

- [ ] **Step 3: Write minimal implementation** — append to `internal/setup/setup.go`

```go
// deleteKeychainPIN removes the remembered PIN; a package var so tests never
// touch the real login Keychain.
var deleteKeychainPIN = func() bool {
	return exec.Command("security", "delete-generic-password", "-s", config.KeychainService).Run() == nil
}

// Run performs a full setup (or update): copy the module to the config dir,
// fetch the cert, and register the module into the default Firefox profile.
// The Firefox and profile pre-conditions are checked BEFORE any mutation, so a
// blocked setup leaves nothing half-written.
func Run(userID string) (*Status, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, coded("no_credential", "no userID provided")
	}
	if firefoxRunning() {
		return nil, coded("firefox_running", "please quit Firefox first")
	}
	prof, err := defaultFirefoxProfile()
	if err != nil {
		return nil, coded("no_profile", "%v", err)
	}
	// Mutations begin here.
	dylibSrc, err := findDylib()
	if err != nil {
		return nil, coded("unknown", "%v", err)
	}
	stable := filepath.Join(config.Dir(), "libtscloud-pkcs11.dylib")
	if err := copyFile(dylibSrc, stable, 0o755); err != nil {
		return nil, coded("unknown", "copying module: %v", err)
	}
	base := defaultBase
	if cfg, _, _, e := config.Load(); e == nil && cfg.BaseURL != "" {
		base = cfg.BaseURL
	}
	if err := fetchCert(base, userID); err != nil {
		return nil, err // already a CodedError
	}
	if _, err := registerModule(prof, stable); err != nil {
		return nil, coded("unknown", "registering module: %v", err)
	}
	return GetStatus(), nil
}

// Uninstall removes the module from Firefox, clears the remembered PIN, and
// deletes ~/.config/tscloud. Firefox must be closed.
func Uninstall() ([]string, error) {
	if firefoxRunning() {
		return nil, coded("firefox_running", "please quit Firefox first")
	}
	var notes []string
	if prof, err := defaultFirefoxProfile(); err == nil {
		if removed, err := unregisterModule(prof); err == nil && removed {
			notes = append(notes, "Removed the module from Firefox.")
		}
	}
	if deleteKeychainPIN() {
		notes = append(notes, "Removed the remembered PIN from the Keychain.")
	}
	dir := config.Dir()
	if _, err := os.Stat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return notes, coded("unknown", "deleting %s: %v", dir, err)
		}
		notes = append(notes, "Deleted "+dir+".")
	}
	return notes, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/setup/ -v`
Expected: PASS — all `internal/setup` tests, hermetically (no skips; no real Firefox/Keychain touched).

- [ ] **Step 5: Commit**

```bash
git add internal/setup/setup.go internal/setup/setup_test.go
git commit -m "feat(setup): Run + Uninstall orchestration with coded errors"
```

---

### Task 3: `cmd/tscloud-engine` — thin JSON CLI (and delete `cmd/tscloud-app`)

**Files:**
- Create: `cmd/tscloud-engine/main.go`
- Test: `cmd/tscloud-engine/main_test.go`
- Delete: `cmd/tscloud-app/main.go`, `cmd/tscloud-app/main_test.go`

**Interfaces:**
- Consumes: `internal/setup` (`GetStatus`, `Run`, `Uninstall`, `Status`, `CodedError`).
- Produces: CLI `tscloud-engine status|setup --user <id>|uninstall`. `status` prints a bare `Status` JSON; `setup`/`uninstall` print `{ok,message?,error?,code?,status?,notes?}`. Internal `run(args []string, out io.Writer) int` returns the process exit code.

- [ ] **Step 1: Write the failing test** — `cmd/tscloud-engine/main_test.go`

```go
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRun_StatusNoConfig(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())
	var buf bytes.Buffer
	code := run([]string{"status"}, &buf)
	if code != 0 {
		t.Fatalf("status exit=%d", code)
	}
	var s struct {
		Installed bool `json:"installed"`
	}
	if err := json.Unmarshal(buf.Bytes(), &s); err != nil {
		t.Fatalf("status output not JSON: %v\n%s", err, buf.String())
	}
	if s.Installed {
		t.Fatal("installed must be false with no config")
	}
}

func TestRun_Usage(t *testing.T) {
	var buf bytes.Buffer
	if code := run(nil, &buf); code != 2 {
		t.Fatalf("no-args exit=%d, want 2", code)
	}
}

func TestRun_SetupEmptyUser(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())
	var buf bytes.Buffer
	code := run([]string{"setup", "--user", ""}, &buf)
	if code == 0 {
		t.Fatal("empty --user should fail")
	}
	var r struct {
		OK   bool   `json:"ok"`
		Code string `json:"code"`
	}
	json.Unmarshal(buf.Bytes(), &r)
	if r.OK || r.Code != "no_credential" {
		t.Fatalf("want ok=false code=no_credential, got %s", strings.TrimSpace(buf.String()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tscloud-engine/ 2>&1 | head`
Expected: FAIL — `main.go` does not exist.

- [ ] **Step 3: Write minimal implementation** — `cmd/tscloud-engine/main.go`

```go
// Command tscloud-engine is the headless engine behind TransSped.app. It has no
// UI: each subcommand prints one JSON object to stdout. The SwiftUI app (and
// scripts) drive it.
//
//	tscloud-engine status
//	tscloud-engine setup --user <email|phone>
//	tscloud-engine uninstall
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"tscloud/internal/setup"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

type result struct {
	OK      bool          `json:"ok"`
	Message string        `json:"message,omitempty"`
	Error   string        `json:"error,omitempty"`
	Code    string        `json:"code,omitempty"`
	Status  *setup.Status `json:"status,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

func run(args []string, out io.Writer) int {
	if len(args) == 0 {
		writeJSON(out, result{Error: "usage: tscloud-engine status|setup|uninstall", Code: "unknown"})
		return 2
	}
	switch args[0] {
	case "status":
		writeJSON(out, setup.GetStatus())
		return 0
	case "setup":
		fs := flag.NewFlagSet("setup", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		user := fs.String("user", "", "Trans Sped userID (email or phone)")
		if err := fs.Parse(args[1:]); err != nil {
			return emitErr(out, err)
		}
		st, err := setup.Run(*user)
		if err != nil {
			return emitErr(out, err)
		}
		writeJSON(out, result{OK: true, Message: "Setup complete.", Status: st})
		return 0
	case "uninstall":
		notes, err := setup.Uninstall()
		if err != nil {
			return emitErr(out, err)
		}
		writeJSON(out, result{OK: true, Message: "Uninstall complete.", Notes: notes})
		return 0
	default:
		writeJSON(out, result{Error: "unknown command: " + args[0], Code: "unknown"})
		return 2
	}
}

func emitErr(out io.Writer, err error) int {
	r := result{Error: err.Error(), Code: "unknown"}
	var ce *setup.CodedError
	if errors.As(err, &ce) {
		r.Code = ce.Code
		r.Error = ce.Message
	}
	writeJSON(out, r)
	return 1
}

func writeJSON(out io.Writer, v any) {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
```

- [ ] **Step 4: Delete the superseded app command**

Run: `git rm cmd/tscloud-app/main.go cmd/tscloud-app/main_test.go`
Expected: both files removed. (Their register/unregister tests now live in `internal/setup`.)

- [ ] **Step 5: Run tests + full build to verify it passes**

Run: `go test ./cmd/tscloud-engine/ ./internal/setup/ -v && go build ./... 2>&1 | grep -v "duplicate libraries"`
Expected: PASS for both packages; build succeeds (only the benign duplicate-libraries warning).

- [ ] **Step 6: Commit**

```bash
git add cmd/tscloud-engine/ cmd/tscloud-app/
git commit -m "feat(engine): tscloud-engine JSON CLI; remove osascript tscloud-app"
```

---

### Task 4: SwiftUI — `Engine.swift` (models + process runner)

**Files:**
- Create: `app/Engine.swift`

**Interfaces:**
- Consumes: the `tscloud-engine` binary in the app bundle's Resources.
- Produces: `EngineStatus` (Codable, mirrors `setup.Status` JSON), `EngineResult` (Codable, mirrors the result envelope), `enum Engine { static func status() async -> EngineStatus?; static func setup(user:) async -> EngineResult; static func uninstall() async -> EngineResult }`, and free func `appVersion() -> String`.

- [ ] **Step 1: Write `app/Engine.swift`**

```swift
import Foundation

/// Mirrors the bare JSON printed by `tscloud-engine status`.
struct EngineStatus: Codable {
    var installed = false
    var account = ""
    var credentialID = ""
    var label = ""
    var certNotAfter = ""
    var certSubject = ""
    var moduleRegistered = false
    var firefoxRunning = false
    var firefoxProfile = ""
}

/// Mirrors the result envelope printed by `tscloud-engine setup|uninstall`.
struct EngineResult: Codable {
    var ok = false
    var message: String?
    var error: String?
    var code: String?
    var status: EngineStatus?
    var notes: [String]?
}

/// Runs the bundled Go engine and decodes its JSON.
enum Engine {
    private static func binaryURL() -> URL? {
        Bundle.main.url(forResource: "tscloud-engine", withExtension: nil)
            ?? Bundle.main.resourceURL?.appendingPathComponent("tscloud-engine")
    }

    private static func run(_ args: [String]) async -> Data? {
        guard let bin = binaryURL() else { return nil }
        return await withCheckedContinuation { cont in
            let p = Process()
            p.executableURL = bin
            p.arguments = args
            let out = Pipe()
            p.standardOutput = out
            p.standardError = Pipe()
            p.terminationHandler = { _ in
                cont.resume(returning: out.fileHandleForReading.readDataToEndOfFile())
            }
            do { try p.run() } catch { cont.resume(returning: nil) }
        }
    }

    static func status() async -> EngineStatus? {
        guard let d = await run(["status"]) else { return nil }
        return try? JSONDecoder().decode(EngineStatus.self, from: d)
    }

    static func setup(user: String) async -> EngineResult {
        await envelope(["setup", "--user", user])
    }

    static func uninstall() async -> EngineResult {
        await envelope(["uninstall"])
    }

    private static func envelope(_ args: [String]) async -> EngineResult {
        guard let d = await run(args),
              let r = try? JSONDecoder().decode(EngineResult.self, from: d)
        else {
            return EngineResult(ok: false, error: "The engine did not respond.", code: "unknown")
        }
        return r
    }
}

/// CFBundleShortVersionString, e.g. "0.0.2".
func appVersion() -> String {
    (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String) ?? "?"
}
```

- [ ] **Step 2: Verify it type-checks**

Run: `swiftc -parse-as-library -target arm64-apple-macos13 -typecheck app/Engine.swift`
Expected: no output (exit 0) — compiles clean.

- [ ] **Step 3: Commit**

```bash
git add app/Engine.swift
git commit -m "feat(app): SwiftUI Engine wrapper (models + process runner)"
```

---

### Task 5: SwiftUI — window views (`AboutView`, `ContentView`, `TransSpedApp`)

**Files:**
- Create: `app/AboutView.swift`, `app/ContentView.swift`, `app/TransSpedApp.swift`

**Interfaces:**
- Consumes: `Engine`, `EngineStatus`, `EngineResult`, `appVersion()` from Task 4.
- Produces: `@main struct TransSpedApp` (the app entry). No exported symbols other tasks consume.

- [ ] **Step 1: Write `app/AboutView.swift`**

```swift
import SwiftUI

struct AboutView: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 12) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 72, height: 72)
            Text("TransSped").font(.title2).bold()
            Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
            Text("Log in to ANAF SPV from macOS Firefox using your Trans Sped cloud qualified certificate.")
                .multilineTextAlignment(.center).font(.callout)
            Text("Signing is delegated to the Trans Sped cloud — no private key is ever stored on this Mac.")
                .multilineTextAlignment(.center).font(.caption).foregroundStyle(.secondary)
            Link("github.com/guness/transsped-mac",
                 destination: URL(string: "https://github.com/guness/transsped-mac")!)
                .font(.callout)
            Button("Close") { dismiss() }.keyboardShortcut(.defaultAction)
        }
        .padding(24)
        .frame(width: 340)
    }
}
```

- [ ] **Step 2: Write `app/ContentView.swift`**

```swift
import SwiftUI

struct ContentView: View {
    @State private var status: EngineStatus?
    @State private var loading = true
    @State private var busy = false
    @State private var message: String?
    @State private var isError = false
    @State private var userID = ""
    @State private var showAbout = false

    var body: some View {
        VStack(spacing: 16) {
            header
            if loading {
                ProgressView().padding()
            } else if status == nil {
                engineError
            } else if let s = status, s.installed {
                installed(s)
            } else {
                setupCard
            }
            if let m = message {
                Text(m).font(.callout)
                    .foregroundStyle(isError ? Color.red : Color.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(24)
        .frame(width: 380)
        .task { await refresh() }
        .sheet(isPresented: $showAbout) { AboutView() }
    }

    private var header: some View {
        VStack(spacing: 6) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 64, height: 64)
            Text("TransSped").font(.title2).bold()
            Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
        }
    }

    private var engineError: some View {
        VStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange).font(.largeTitle)
            Text("Couldn't run the setup engine.").font(.headline)
            Text("The app may be damaged — reinstall TransSped from the DMG.")
                .font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center)
            Button("Retry") { Task { await refresh() } }
        }
    }

    private var setupCard: some View {
        VStack(spacing: 12) {
            Text("Set up TransSped").font(.headline)
            Text("Enter the email or phone registered with Trans Sped for your cloud certificate.")
                .font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.center)
            TextField("email or phone", text: $userID).textFieldStyle(.roundedBorder)
            Button("Set up") { Task { await doSetup(user: userID) } }
                .buttonStyle(.borderedProminent)
                .disabled(busy || userID.trimmingCharacters(in: .whitespaces).isEmpty)
            if busy { ProgressView() }
        }
    }

    private func installed(_ s: EngineStatus) -> some View {
        VStack(spacing: 14) {
            statusRows(s)
            HStack {
                Button("Update") { Task { await doSetup(user: s.account) } }.disabled(busy)
                Button("Open ANAF login") { openANAF() }
            }
            HStack {
                Button("Uninstall", role: .destructive) { Task { await doUninstall() } }.disabled(busy)
                Button("About") { showAbout = true }
            }
            if busy { ProgressView() }
        }
    }

    private func statusRows(_ s: EngineStatus) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            row(s.moduleRegistered ? "checkmark.circle.fill" : "exclamationmark.triangle.fill",
                s.moduleRegistered ? "Installed in Firefox" : "Not registered in Firefox",
                s.moduleRegistered ? .green : .orange)
            row("person.crop.circle", "Account: \(s.account)", .secondary)
            if !s.certNotAfter.isEmpty {
                row("calendar", "Certificate valid until \(formatDate(s.certNotAfter))", expiryColor(s.certNotAfter))
            }
            if s.firefoxRunning {
                row("exclamationmark.triangle.fill", "Firefox is open — quit it before Update / Uninstall", .orange)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func row(_ icon: String, _ text: String, _ color: Color) -> some View {
        HStack(spacing: 8) {
            Image(systemName: icon).foregroundStyle(color)
            Text(text).font(.callout)
            Spacer()
        }
    }

    // MARK: - actions

    private func refresh() async {
        loading = true
        status = await Engine.status()
        loading = false
    }

    private func doSetup(user: String) async {
        busy = true; message = nil
        let r = await Engine.setup(user: user.trimmingCharacters(in: .whitespaces))
        if r.ok {
            status = r.status ?? (await Engine.status())
            message = r.message; isError = false
        } else {
            message = friendly(r); isError = true
        }
        busy = false
    }

    private func doUninstall() async {
        busy = true; message = nil
        let r = await Engine.uninstall()
        if r.ok {
            status = await Engine.status()
            message = ((r.notes ?? []) + [r.message ?? "Uninstalled."]).joined(separator: "\n")
            isError = false
        } else {
            message = friendly(r); isError = true
        }
        busy = false
    }

    private func openANAF() {
        let url = URL(string: "https://pfinternet.anaf.ro")!
        let firefox = URL(fileURLWithPath: "/Applications/Firefox.app")
        NSWorkspace.shared.open([url], withApplicationAt: firefox,
                                configuration: NSWorkspace.OpenConfiguration()) { _, _ in }
    }

    private func friendly(_ r: EngineResult) -> String {
        switch r.code {
        case "firefox_running": return "Please quit Firefox first, then try again."
        case "no_credential":  return "No certificate was found for this userID. Check it — or you may still need to enroll with Trans Sped (ANAF form 150)."
        case "no_profile":     return "No Firefox profile found. Launch Firefox once, then quit it and try again."
        case "network":        return "Couldn't reach the Trans Sped service. Check your connection and try again."
        default:               return r.error ?? "Something went wrong."
        }
    }

    private func parseDate(_ iso: String) -> Date? {
        ISO8601DateFormatter().date(from: iso)
    }

    private func formatDate(_ iso: String) -> String {
        guard let d = parseDate(iso) else { return iso }
        let f = DateFormatter(); f.dateStyle = .medium; f.timeStyle = .none
        return f.string(from: d)
    }

    private func expiryColor(_ iso: String) -> Color {
        guard let d = parseDate(iso) else { return .secondary }
        let days = d.timeIntervalSinceNow / 86400
        if days < 0 { return .red }
        if days < 30 { return .orange }
        return .secondary
    }
}
```

- [ ] **Step 3: Write `app/TransSpedApp.swift`**

```swift
import SwiftUI

@main
struct TransSpedApp: App {
    var body: some Scene {
        WindowGroup {
            ContentView()
        }
        .windowResizability(.contentSize)
        .commands {
            CommandGroup(replacing: .newItem) {} // no "New Window"
        }
    }
}
```

- [ ] **Step 4: Verify all app sources type-check together**

Run: `swiftc -parse-as-library -target arm64-apple-macos13 -typecheck app/*.swift`
Expected: no output (exit 0).

- [ ] **Step 5: Commit**

```bash
git add app/AboutView.swift app/ContentView.swift app/TransSpedApp.swift
git commit -m "feat(app): SwiftUI window (status, setup, actions, About)"
```

---

### Task 6: Build pipeline — swiftc app + go engine + dylib, signed

**Files:**
- Modify: `scripts/build-app.sh`

**Interfaces:**
- Consumes: `app/*.swift`, `cmd/tscloud-engine`, `cmd/pkcs11`, `scripts/gen-icon.go`.
- Produces: a signed `TransSped.app` whose `Contents/MacOS/TransSped` is the SwiftUI binary, with `tscloud-engine` + `libtscloud-pkcs11.dylib` in `Contents/Resources`.

- [ ] **Step 1: Rewrite `scripts/build-app.sh`**

```bash
#!/usr/bin/env bash
# Builds "TransSped.app" — a native SwiftUI window (Contents/MacOS/TransSped)
# driving the headless Go engine (Contents/Resources/tscloud-engine) and the
# PKCS#11 module (Contents/Resources/libtscloud-pkcs11.dylib).
#
# Signing: ad-hoc by default; set SIGN_ID to a "Developer ID Application: …"
# identity (or its SHA-1) for a hardened-runtime, timestamped, notarizable build.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="TransSped.app"
ARCH="${GOARCH:-arm64}"
SIGN_ID="${SIGN_ID:-}"
TARGET="arm64-apple-macos13"

sign() {
  if [ -n "$SIGN_ID" ]; then
    codesign --force --options runtime --timestamp -s "$SIGN_ID" "$1"
  else
    codesign --force -s - "$1"
  fi
}

echo "==> building PKCS#11 module (libtscloud-pkcs11.dylib)"
CGO_ENABLED=1 GOARCH="$ARCH" go build -buildmode=c-shared -o libtscloud-pkcs11.dylib ./cmd/pkcs11/

echo "==> assembling $APP"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

echo "==> building Go engine (tscloud-engine)"
GOARCH="$ARCH" go build -o "$APP/Contents/Resources/tscloud-engine" ./cmd/tscloud-engine/
cp libtscloud-pkcs11.dylib "$APP/Contents/Resources/"

echo "==> compiling SwiftUI app (TransSped)"
swiftc -O -parse-as-library -target "$TARGET" \
  -framework SwiftUI -framework AppKit \
  -o "$APP/Contents/MacOS/TransSped" app/*.swift

echo "==> rendering app icon (AppIcon.icns)"
ICONSET="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$ICONSET"
MASTER="$(mktemp -d)/icon_1024.png"
go run scripts/gen-icon.go "$MASTER"
for spec in "16:16x16" "32:16x16@2x" "32:32x32" "64:32x32@2x" \
            "128:128x128" "256:128x128@2x" "256:256x256" "512:256x256@2x" \
            "512:512x512" "1024:512x512@2x"; do
  px="${spec%%:*}"; name="${spec##*:}"
  sips -z "$px" "$px" "$MASTER" --out "$ICONSET/icon_${name}.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns"

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>TransSped</string>
  <key>CFBundleDisplayName</key><string>TransSped</string>
  <key>CFBundleIdentifier</key><string>ro.transsped.macos</string>
  <key>CFBundleVersion</key><string>0.0.2</string>
  <key>CFBundleShortVersionString</key><string>0.0.2</string>
  <key>CFBundleExecutable</key><string>TransSped</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

echo "==> codesigning ($([ -n "$SIGN_ID" ] && echo "$SIGN_ID" || echo "ad-hoc"))"
sign "$APP/Contents/Resources/libtscloud-pkcs11.dylib"
sign "$APP/Contents/Resources/tscloud-engine"
sign "$APP/Contents/MacOS/TransSped"
sign "$APP"
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true

echo "built: $APP  (double-click it, or run: open \"$APP\")"
if [ -z "$SIGN_ID" ]; then
  echo "note: ad-hoc signed — opens on THIS Mac. To share, see docs/PACKAGING.md."
fi
```

- [ ] **Step 2: Build the app**

Run: `./scripts/build-app.sh 2>&1 | grep -vE "duplicate libraries|^# tscloud"`
Expected: ends with `built: TransSped.app`, no errors.

- [ ] **Step 3: Verify structure, signature, and that it launches with a window**

Run:
```bash
find "TransSped.app" -type f | sort
codesign --verify --deep --strict --verbose=2 "TransSped.app"
/usr/libexec/PlistBuddy -c "Print CFBundleExecutable" "TransSped.app/Contents/Info.plist"
open "TransSped.app"; sleep 2; osascript -e 'tell application "System Events" to (name of processes) contains "TransSped"'
```
Expected: bundle contains `MacOS/TransSped`, `Resources/tscloud-engine`, `Resources/libtscloud-pkcs11.dylib`, `Resources/AppIcon.icns`; codesign "valid on disk" + "satisfies its Designated Requirement"; `CFBundleExecutable` = `TransSped`; the `osascript` check prints `true` (a window/process is running). Manually confirm the window shows the header, status rows (or the set-up card), and that **About** opens.

- [ ] **Step 4: Commit**

```bash
git add scripts/build-app.sh
git commit -m "build(app): swiftc SwiftUI window + bundled Go engine; bump to 0.0.2"
```

---

### Task 7: Docs — window-based install steps

**Files:**
- Modify: `README.md`, `docs/RUNBOOK.md`, `scripts/smoke-test.md`

**Interfaces:**
- Consumes: nothing (documentation).
- Produces: updated user-facing instructions describing the window (Set up field, status, buttons) instead of the osascript userID prompt.

- [ ] **Step 1: Update `README.md` Install section**

Replace the "Install (the app)" numbered steps with (keep surrounding sections):

```markdown
## Install (the app)

1. **Quit Firefox** (a security module can't be added while it's running).
2. Open **`TransSped.app`** (double-click, or `open "TransSped.app"`). A small
   window appears.
3. First run shows **Set up TransSped** — enter your **Trans Sped userID** (the
   email or phone registered for your cloud certificate) and click **Set up**.
   The app fetches your certificate and registers the PKCS#11 module into your
   normal Firefox profile.
4. The window then shows your status: **Installed in Firefox**, your account,
   and the certificate's expiry. From here you can **Update** (re-fetch),
   **Open ANAF login**, **Uninstall**, or view **About**.

Then use Firefox as usual (see [Daily use](#daily-use)).
```

- [ ] **Step 2: Update the README Uninstall subsection**

Replace the `-uninstall` CLI block with:

```markdown
### Uninstall

Open **TransSped**, then click **Uninstall** (with Firefox quit). It unregisters
the `TransSpedCloud` module from Firefox and deletes `~/.config/tscloud`
(including any remembered PIN). You can also unload it manually from Firefox →
Settings → Privacy & Security → **Security Devices** → **Unload**.
```

- [ ] **Step 3: Update `docs/RUNBOOK.md` Step 1**

Replace the "Step 1 — Install with the app" body with:

```markdown
## Step 1 — Install with the app

1. **Quit Firefox** — a security module can't be added while it's running.
2. Open **`TransSped.app`** (double-click, or `open "TransSped.app"`).
3. In the window's **Set up** card, enter your **Trans Sped userID** (email or
   phone) and click **Set up**.
4. The window switches to the status view: **Installed in Firefox**, your
   account, and the certificate expiry. Behind the scenes the app fetched your
   certificate to `~/.config/tscloud/`, copied the module there, and registered
   it into your default profile's `pkcs11.txt`.

(The engine is also runnable headlessly for scripting/CI:
`TransSped.app/Contents/Resources/tscloud-engine status|setup --user <id>|uninstall`.)
```

- [ ] **Step 4: Update `scripts/smoke-test.md` closing note**

Append a short section documenting the manual window smoke test:

```markdown
## App window smoke test (manual)

After `./scripts/build-app.sh`:

1. `open "TransSped.app"` → a window appears.
2. With no `~/.config/tscloud`, it shows the **Set up** card (userID field).
   With a config present, it shows status rows (Installed in Firefox, Account,
   Certificate valid until …) and the Update / Open ANAF / Uninstall / About
   buttons.
3. Click **About** → the sheet shows name, version, description, and the GitHub
   link. Click **Close**.
4. `TransSped.app/Contents/Resources/tscloud-engine status` prints a JSON
   object; `installed` reflects whether a config exists.
```

- [ ] **Step 5: Verify docs have no stale references + full test suite**

Run:
```bash
grep -rn "tscloud-app\|--args -uninstall\|osascript userID" README.md docs/RUNBOOK.md || echo "(no stale refs)"
go test ./... 2>&1 | tail -12
```
Expected: no stale references; all Go tests pass.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/RUNBOOK.md scripts/smoke-test.md
git commit -m "docs: window-based install/uninstall + engine CLI notes"
```

---

## Notes for the implementer

- **Releasing is out of scope** for this plan. When ready, follow
  `docs/PACKAGING.md`: build with `SIGN_ID`, then `SIGN_ID=… AC_PROFILE=transsped-notary ./scripts/make-dmg.sh`, tag `v0.0.2`, `gh release create`. The version is already bumped to 0.0.2 in Task 6.
- **PIN/OTP is untouched** — it is collected by the dylib inside Firefox at sign
  time, not by this app.
- If `swiftc` reports an unavailable API, confirm `-target arm64-apple-macos13`
  is set; all SwiftUI APIs used here (`.task`, `foregroundStyle`, `Button(role:)`,
  `.windowResizability`, `.sheet`) require macOS 13.
```
