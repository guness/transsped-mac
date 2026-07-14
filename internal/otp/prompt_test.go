package otp

import "testing"

func TestEscape(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text unchanged", "please enter code", "please enter code"},
		{"lone quote", `"`, `\"`},
		{"lone backslash", `\`, `\\`},
		// Breakout case: input is backslash + quote (2 bytes). Escaping
		// backslashes first doubles the backslash, then the quote is
		// escaped, yielding backslash-backslash-backslash-quote (4 bytes).
		{"backslash-quote breakout", `\"`, `\\\"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := escape(c.in); got != c.want {
				t.Errorf("escape(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
