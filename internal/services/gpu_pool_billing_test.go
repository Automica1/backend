package services

import (
	"testing"

	"chi-mongo-backend/internal/models"
)

func TestSessionCreditBreakdown(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		sess           *models.GPUPoolSession
		wantBooked     int
		wantDebited    int
		wantGpuTime    int
	}{
		{
			name: "reconnect — startup booked not debited again",
			sess: &models.GPUPoolSession{CreditsStartupCharged: 20, CreditsCharged: 4},
			wantBooked: 20, wantDebited: 0, wantGpuTime: 4,
		},
		{
			name: "fresh start with meter",
			sess: &models.GPUPoolSession{CreditsStartupCharged: 20, CreditsCharged: 24},
			wantBooked: 20, wantDebited: 20, wantGpuTime: 4,
		},
		{
			name: "startup only",
			sess: &models.GPUPoolSession{CreditsStartupCharged: 20, CreditsCharged: 20},
			wantBooked: 20, wantDebited: 20, wantGpuTime: 0,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			booked, debited, gpuTime := sessionCreditBreakdown(tc.sess)
			if booked != tc.wantBooked || debited != tc.wantDebited || gpuTime != tc.wantGpuTime {
				t.Fatalf("sessionCreditBreakdown() = (%d,%d,%d), want (%d,%d,%d)",
					booked, debited, gpuTime, tc.wantBooked, tc.wantDebited, tc.wantGpuTime)
			}
		})
	}
}
