package services

import (
	"context"
	"testing"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/models"
)

func TestReconnectWindowOrphanBoot(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	stopped := now.Add(-2 * time.Minute)
	svc := &gpuPoolService{cfg: &config.Config{}}
	pool := &models.GPUPool{
		State:    models.GPUPoolStateProvisioning,
		RefCount: 0,
		Sessions: []models.GPUPoolSession{
			{
				UserID:                "user-1",
				StartedAt:             now.Add(-3 * time.Minute),
				StoppedAt:             &stopped,
				CreditsStartupCharged: 20,
			},
		},
	}
	eligible, until, skip := svc.reconnectWindow(context.Background(), pool, "user-1")
	if !eligible || !skip || until == nil {
		t.Fatalf("expected reconnect window, got eligible=%v skip=%v until=%v", eligible, skip, until)
	}
}

func TestReconnectWindowIdlePoolAfterDestroy(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	stopped := now.Add(-2 * time.Minute)
	svc := &gpuPoolService{cfg: &config.Config{}}
	pool := &models.GPUPool{
		State:    models.GPUPoolStateIdle,
		RefCount: 0,
		Sessions: []models.GPUPoolSession{
			{
				UserID:                "user-1",
				StoppedAt:             &stopped,
				CreditsStartupCharged: 6,
			},
		},
	}
	eligible, until, skip := svc.reconnectWindow(context.Background(), pool, "user-1")
	if !eligible || !skip || until == nil {
		t.Fatalf("expected reconnect after destroy (idle pool), got eligible=%v skip=%v until=%v", eligible, skip, until)
	}
}

func TestReconnectWindowExpired(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	stopped := now.Add(-10 * time.Minute)
	svc := &gpuPoolService{cfg: &config.Config{}}
	pool := &models.GPUPool{
		State:    models.GPUPoolStateReady,
		RefCount: 0,
		Sessions: []models.GPUPoolSession{
			{
				UserID:                "user-1",
				StoppedAt:             &stopped,
				CreditsStartupCharged: 20,
			},
		},
	}
	eligible, _, skip := svc.reconnectWindow(context.Background(), pool, "user-1")
	if eligible || skip {
		t.Fatalf("expected no reconnect after cooldown, got eligible=%v skip=%v", eligible, skip)
	}
}
