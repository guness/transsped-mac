package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tscloud/internal/config"
)

// seedProfile writes a minimal NSS-style pkcs11.txt (just the built-in internal
// module record) into a temp dir and returns the dir, standing in for a real
// Firefox profile.
func seedProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	seed := "library=\nname=NSS Internal PKCS #11 Module\nparameters=configdir='sql:.'\nNSS=Flags=internal\n"
	if err := os.WriteFile(filepath.Join(dir, "pkcs11.txt"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRegisterUnregisterRoundTrip proves the setup app's register step adds our
// module, is idempotent, and that uninstall removes exactly our record while
// leaving the internal NSS module record intact — all on a throwaway profile.
func TestRegisterUnregisterRoundTrip(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())
	prof := seedProfile(t)
	dylib := filepath.Join(config.Dir(), "libtscloud-pkcs11.dylib")

	added, err := registerModule(prof, dylib)
	if err != nil || !added {
		t.Fatalf("registerModule: added=%v err=%v", added, err)
	}
	if added2, _ := registerModule(prof, dylib); added2 {
		t.Fatal("registerModule must be idempotent (already registered)")
	}
	if data, _ := os.ReadFile(filepath.Join(prof, "pkcs11.txt")); !strings.Contains(string(data), "name="+moduleName) {
		t.Fatalf("module missing after register:\n%s", data)
	}

	removed, err := unregisterModule(prof)
	if err != nil || !removed {
		t.Fatalf("unregisterModule: removed=%v err=%v", removed, err)
	}
	data, _ := os.ReadFile(filepath.Join(prof, "pkcs11.txt"))
	if strings.Contains(string(data), moduleName) {
		t.Fatalf("module still present after unregister:\n%s", data)
	}
	if !strings.Contains(string(data), "NSS Internal PKCS #11 Module") {
		t.Fatalf("unregister clobbered the internal NSS module record:\n%s", data)
	}
	if removed2, _ := unregisterModule(prof); removed2 {
		t.Fatal("unregisterModule must be idempotent (nothing left to remove)")
	}
}

// TestUnregisterCanonicalForm covers NSS having rewritten pkcs11.txt into its
// own record form (extra fields, different ordering): removal keys off the
// name= line, not the exact block the installer wrote.
func TestUnregisterCanonicalForm(t *testing.T) {
	t.Setenv("TSCLOUD_DIR", t.TempDir())
	dir := t.TempDir()
	canonical := "library=\nname=NSS Internal PKCS #11 Module\nNSS=Flags=internal\n\n" +
		"library=/Users/x/.config/tscloud/libtscloud-pkcs11.dylib\nname=TransSpedCloud\nNSS=trustOrder=75\n"
	if err := os.WriteFile(filepath.Join(dir, "pkcs11.txt"), []byte(canonical), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := unregisterModule(dir)
	if err != nil || !removed {
		t.Fatalf("unregisterModule: removed=%v err=%v", removed, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "pkcs11.txt")); strings.Contains(string(data), moduleName) {
		t.Fatalf("canonical-form record not removed:\n%s", data)
	}
}
