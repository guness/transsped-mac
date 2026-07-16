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
