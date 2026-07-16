package otp

import (
	"strings"
	"testing"
)

// TestDialogCopy checks both languages return a complete, distinct string set
// (English source vs Romanian translation) so neither leaves a field blank.
func TestDialogCopy(t *testing.T) {
	en := dialogCopy(langEN)
	ro := dialogCopy(langRO)

	if en.title == "" || en.pinField == "" || en.otpField == "" || en.remember == "" {
		t.Fatalf("english copy has empty fields: %+v", en)
	}
	if ro.title == "" || ro.pinField == "" || ro.otpField == "" || ro.remember == "" {
		t.Fatalf("romanian copy has empty fields: %+v", ro)
	}
	if en.title == ro.title || en.remember == ro.remember || en.pinField == ro.pinField {
		t.Fatal("romanian copy is identical to english — not translated")
	}
	// The button used to detect "remember" in the fallback must be non-empty and
	// distinct from the plain OK button in each language.
	if en.rememberOK == en.ok || ro.rememberOK == ro.ok {
		t.Fatal("rememberOK must differ from ok so the fallback can detect it")
	}
	if !strings.Contains(ro.remember, "PIN") {
		t.Errorf("unexpected romanian remember label: %q", ro.remember)
	}
}
