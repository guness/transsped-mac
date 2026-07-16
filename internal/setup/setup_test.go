package setup

import (
	"errors"
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

// stubKeychain replaces the Keychain delete so tests never touch the real login
// Keychain.
func stubKeychain(t *testing.T) {
	t.Helper()
	orig := deleteKeychainPIN
	deleteKeychainPIN = func() bool { return false }
	t.Cleanup(func() { deleteKeychainPIN = orig })
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
