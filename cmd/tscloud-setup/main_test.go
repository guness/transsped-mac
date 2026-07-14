package main

import "testing"

func TestClean(t *testing.T) {
	t.Run("raw single-line base64 passes through unchanged", func(t *testing.T) {
		in := "QUJDRA=="
		got := clean(in)
		if got != in {
			t.Errorf("clean(%q) = %q, want %q", in, got, in)
		}
	})

	t.Run("PEM-armored input strips armor and joins lines", func(t *testing.T) {
		in := "-----BEGIN CERTIFICATE-----\nQUJD\nRA==\n-----END CERTIFICATE-----"
		want := "QUJDRA=="
		got := clean(in)
		if got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	})

	t.Run("blank lines are dropped and remaining lines concatenated", func(t *testing.T) {
		in := "QUJD\n\nRA==\n\n"
		want := "QUJDRA=="
		got := clean(in)
		if got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	})
}
