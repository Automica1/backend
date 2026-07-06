package services

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

func (s *gpuPoolService) reconnectDestroyKey(serviceTag string) string {
	return "gpu-pool:" + serviceTag + ":reconnect_destroy"
}

// poolReconnectUntil is the latest reconnect-until among recent stopped sessions (any user).
func (s *gpuPoolService) poolReconnectUntil(pool *models.GPUPool) (*time.Time, bool) {
	if pool == nil {
		return nil, false
	}
	now := time.Now().UTC()
	cooldown := s.reconnectCooldown()
	var latest *time.Time
	for i := range pool.Sessions {
		sess := &pool.Sessions[i]
		if sess.StoppedAt == nil || sess.CreditsStartupCharged <= 0 || sess.StartupRefunded {
			continue
		}
		until := sess.StoppedAt.Add(cooldown)
		if now.Before(until) {
			if latest == nil || until.After(*latest) {
				t := until
				latest = &t
			}
		}
	}
	return latest, latest != nil
}

func (s *gpuPoolService) poolInReconnectWindow(pool *models.GPUPool) bool {
	_, ok := s.poolReconnectUntil(pool)
	return ok
}

func (s *gpuPoolService) scheduleReconnectDestroy(ctx context.Context, serviceTag, serviceName string, runAfter time.Time) error {
	key := s.reconnectDestroyKey(serviceTag)
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolDestroy, EnqueueJobOptions{
		IdempotencyKey: key,
		RunAfter:       runAfter,
		Payload: map[string]any{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
		},
		MaxAttempts: 3,
	})
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrValidation {
		active, activeErr := s.jobSvc.HasActiveByIdempotencyKey(ctx, key)
		if activeErr != nil {
			return activeErr
		}
		if active {
			return nil
		}
	}
	return err
}

func (s *gpuPoolService) abortProvisionAndDestroy(ctx context.Context, serviceTag, serviceName string) error {
	provisionKey := s.provisionKey(serviceTag)
	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, provisionKey)
	_, _ = s.jobSvc.CancelRunningByIdempotencyKey(ctx, provisionKey)
	return s.enqueueDestroy(ctx, serviceTag, serviceName)
}

func (s *gpuPoolService) afterEarlyStopDuringProvision(ctx context.Context, pool *models.GPUPool, serviceName string) error {
	if pool == nil || pool.RefCount > 0 {
		return nil
	}
	if pool.TeardownOnUserStop() {
		until, inWindow := s.poolReconnectUntil(pool)
		if inWindow && until != nil {
			return s.scheduleReconnectDestroy(ctx, pool.ServiceTag, serviceName, *until)
		}
		return s.abortProvisionAndDestroy(ctx, pool.ServiceTag, serviceName)
	}
	return s.abortProvisionAndDestroy(ctx, pool.ServiceTag, serviceName)
}

// HandleProvisionNoSessions is called by the worker when refCount=0 mid-provision.
// Returns true when bootstrap/pipeline should continue (B4 inside reconnect window only).
func (s *gpuPoolService) HandleProvisionNoSessions(ctx context.Context, serviceTag string, afterPipeline bool) (bool, error) {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil || pool == nil || pool.RefCount > 0 {
		return pool != nil && pool.RefCount > 0, err
	}
	serviceName, svcErr := s.resolveServiceName(serviceTag)
	if svcErr != nil {
		return false, nil
	}

	if afterPipeline {
		if pool.State == models.GPUPoolStateReady {
			_, _ = s.poolRepo.Update(ctx, serviceTag, map[string]any{
				"state":   models.GPUPoolStateProvisioning,
				"readyAt": nil,
			})
		}
		until, inWindow := s.poolReconnectUntil(pool)
		if inWindow && until != nil {
			return false, s.scheduleReconnectDestroy(ctx, serviceTag, serviceName, *until)
		}
		return false, s.enqueueDestroy(ctx, serviceTag, serviceName)
	}

	if !pool.TeardownOnUserStop() {
		return false, nil
	}

	until, inWindow := s.poolReconnectUntil(pool)
	if inWindow && until != nil {
		_ = s.scheduleReconnectDestroy(ctx, serviceTag, serviceName, *until)
		return true, nil
	}
	return false, s.enqueueDestroy(ctx, serviceTag, serviceName)
}
