package services

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
)

func (s *gpuPoolService) policyFor(ctx context.Context, serviceTag string) *models.GPUProvisionConfig {
	if s.policySvc == nil || serviceTag == "" {
		return nil
	}
	cfg, err := s.policySvc.GetOrDefault(ctx, serviceTag)
	if err != nil || cfg == nil {
		return nil
	}
	return cfg
}

func (s *gpuPoolService) gracePeriodFor(ctx context.Context, serviceTag string) time.Duration {
	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.Lifecycle.GracePeriodMin > 0 {
		return time.Duration(cfg.Lifecycle.GracePeriodMin) * time.Minute
	}
	minutes := s.cfg.Worker.GracePeriodMin
	if minutes <= 0 {
		minutes = 5
	}
	return time.Duration(minutes) * time.Minute
}

func (s *gpuPoolService) gracePeriodSecFor(ctx context.Context, serviceTag string) int {
	sec := int(s.gracePeriodFor(ctx, serviceTag).Seconds())
	if sec <= 0 {
		return 300
	}
	return sec
}

func (s *gpuPoolService) reconnectCooldownFor(ctx context.Context, serviceTag string) time.Duration {
	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.Lifecycle.ReconnectCooldownSec > 0 {
		return time.Duration(cfg.Lifecycle.ReconnectCooldownSec) * time.Second
	}
	if s.cfg.Worker.ReconnectCooldownSec > 0 {
		return time.Duration(s.cfg.Worker.ReconnectCooldownSec) * time.Second
	}
	return 5 * time.Minute
}

func (s *gpuPoolService) userRetryHintMin(ctx context.Context, serviceTag string) int {
	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.Lifecycle.UserRetryHintMin > 0 {
		return cfg.Lifecycle.UserRetryHintMin
	}
	return 15
}

func (s *gpuPoolService) provisionMaxAttempts(ctx context.Context, serviceTag string) int {
	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.Retries.ProvisionMaxAttempts > 0 {
		return cfg.Retries.ProvisionMaxAttempts
	}
	return 3
}

func (s *gpuPoolService) destroyMaxAttempts(ctx context.Context, serviceTag string) int {
	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.Retries.DestroyMaxAttempts > 0 {
		return cfg.Retries.DestroyMaxAttempts
	}
	return 3
}

func (s *gpuPoolService) stuckProvisionThresholds(ctx context.Context, serviceTag string) (noJob time.Duration, zombie time.Duration) {
	cfg := s.policyFor(ctx, serviceTag)
	if cfg != nil {
		if cfg.Lifecycle.StuckProvisionNoJobMin > 0 {
			noJob = time.Duration(cfg.Lifecycle.StuckProvisionNoJobMin) * time.Minute
		}
		if cfg.Lifecycle.StuckProvisionZombieMin > 0 {
			zombie = time.Duration(cfg.Lifecycle.StuckProvisionZombieMin) * time.Minute
		}
	}
	if noJob == 0 {
		noJob = 13 * time.Minute
	}
	if zombie == 0 {
		zombie = 22 * time.Minute
	}
	return noJob, zombie
}

func (s *gpuPoolService) sanitizePoolError(ctx context.Context, serviceTag, raw string) string {
	return SanitizeGPUPoolUserError(raw, s.userRetryHintMin(ctx, serviceTag))
}

func (s *gpuPoolService) poolErrorUpdate(ctx context.Context, serviceTag, raw string) map[string]any {
	if raw == "" {
		return map[string]any{"lastError": "", "lastErrorRaw": ""}
	}
	return map[string]any{
		"lastError":    s.sanitizePoolError(ctx, serviceTag, raw),
		"lastErrorRaw": raw,
	}
}
