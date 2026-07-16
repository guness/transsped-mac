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
