package token

import (
	"testing"

	"tscloud/internal/csc"
)

// TestFindObjects_NegativeMaxNoPanic guards against a regression where a
// PKCS#11 caller passes an unsigned "unlimited" max that casts to a negative
// Go int, which previously panicked on the b.find[:n] slice. A negative max
// now means "return all remaining".
func TestFindObjects_NegativeMaxNoPanic(t *testing.T) {
	b := NewBackend(BuildObjects(sampleLeaf(t), nil), &csc.Signer{})

	if err := b.FindObjectsInit(1, nil); err != nil {
		t.Fatalf("FindObjectsInit: %v", err)
	}
	hs, _, err := b.FindObjects(1, -1)
	if err != nil {
		t.Fatalf("FindObjects(-1): %v", err)
	}
	if len(hs) != 2 {
		t.Fatalf("want 2 handles (cert+privkey), got %d", len(hs))
	}

	if err := b.FindObjectsInit(1, nil); err != nil {
		t.Fatalf("FindObjectsInit (re-init): %v", err)
	}
	hs, _, err = b.FindObjects(1, 1)
	if err != nil {
		t.Fatalf("FindObjects(1): %v", err)
	}
	if len(hs) != 1 {
		t.Fatalf("want 1 handle (respecting positive max), got %d", len(hs))
	}
}
