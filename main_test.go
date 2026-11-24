package main

import (
	"os"
	"testing"
)

// askpassValue must fire only when ssh runs us as SSH_ASKPASS (the marker
// env var), and then print the password verbatim. ssh passes the prompt as
// argv[0] with no other argument, so the arguments must never decide this:
// a regression that also checks os.Args breaks password auth entirely.
func TestAskpassValue(t *testing.T) {
	for _, tc := range []struct {
		name     string
		marker   string
		password string
		want     string
		wantOK   bool
	}{
		{"not askpass", "", "p@ss", "", false},
		{"marker off", "0", "p@ss", "", false},
		{"askpass", "1", "p@ss", "p@ss", true},
		{"askpass with special chars", "1", "p@ss w0rd!#$", "p@ss w0rd!#$", true},
		{"askpass empty password", "1", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KERO_SSH_ASKPASS", tc.marker)
			t.Setenv("KERO_SSH_PASSWORD", tc.password)
			// Explicitly set no useful arguments: askpass must work when
			// ssh invokes it with the prompt as argv[0] and nothing else.
			os.Args = []string{"Enter password for devbox:"}
			got, ok := askpassValue()
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("askpassValue() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
