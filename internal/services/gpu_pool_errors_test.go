package services

import "testing"

func TestSanitizeGPUPoolUserError(t *testing.T) {
	raw := "scripts/e2e_bootstrap.sh: exit status 1\nstderr: /usr/bin/python3: No module named pip"
	got := SanitizeGPUPoolUserError(raw, 15)
	if got == raw || got == "" {
		t.Fatalf("expected sanitized message, got %q", got)
	}
}

func TestIsTerminalProvisionError(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"E2E node 321707 failed (status=Failed recreate)", true},
		{"E2E node 1 stalled in status=Creating for 360s", true},
		{"Missing dist/images.tar.gz — building from source. THIS NEEDS INTERNET.", true},
		{"failed to copy: failed to send write: error reading from server: EOF", true},
		{"412 Precondition Failed", false},
		{"SSH not reachable on 1.2.3.4", false},
	}
	for _, tc := range cases {
		if got := IsTerminalProvisionError(tc.raw); got != tc.want {
			t.Fatalf("IsTerminalProvisionError(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}
