package services

import "testing"

func TestGpuPoolReattachedSession(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		userActive        bool
		hasActiveSession  bool
		freshSessionStart bool
		want              bool
	}{
		{name: "fresh start", userActive: true, hasActiveSession: true, freshSessionStart: true, want: false},
		{name: "status poll", userActive: true, hasActiveSession: true, freshSessionStart: false, want: true},
		{name: "idempotent start", userActive: true, hasActiveSession: true, freshSessionStart: false, want: true},
		{name: "no session", userActive: false, hasActiveSession: false, freshSessionStart: false, want: false},
		{name: "inactive with session record", userActive: false, hasActiveSession: true, freshSessionStart: false, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := gpuPoolReattachedSession(tc.userActive, tc.hasActiveSession, tc.freshSessionStart)
			if got != tc.want {
				t.Fatalf("gpuPoolReattachedSession() = %v, want %v", got, tc.want)
			}
		})
	}
}
