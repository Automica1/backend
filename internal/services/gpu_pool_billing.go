package services

import (
	"context"
	"fmt"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

func (s *gpuPoolService) creditsPerMinute() int {
	if s.cfg.Worker.CreditsPerMin > 0 {
		return s.cfg.Worker.CreditsPerMin
	}
	return 2
}

func (s *gpuPoolService) minStartCredits() int {
	if s.cfg.Worker.MinStartCredits > 0 {
		return s.cfg.Worker.MinStartCredits
	}
	return 30
}

func (s *gpuPoolService) startupCredits() int {
	if s.cfg.Worker.StartupCredits > 0 {
		return s.cfg.Worker.StartupCredits
	}
	return 20
}

func (s *gpuPoolService) meterIntervalSec() int {
	sec := s.cfg.Worker.MeterIntervalSec
	if sec <= 0 {
		sec = 60
	}
	return sec
}

func (s *gpuPoolService) meterInterval() time.Duration {
	return time.Duration(s.meterIntervalSec()) * time.Second
}

func nextMeterChargeAt(sess *models.GPUPoolSession, interval time.Duration) *time.Time {
	if sess == nil || sess.BillingStartedAt == nil || sess.LastMeteredAt == nil {
		return nil
	}
	t := sess.LastMeteredAt.Add(interval)
	return &t
}

func (s *gpuPoolService) reconnectCooldown() time.Duration {
	if s.cfg.Worker.ReconnectCooldownSec > 0 {
		return time.Duration(s.cfg.Worker.ReconnectCooldownSec) * time.Second
	}
	return 5 * time.Minute
}

func poolReusableForReconnect(state models.GPUPoolState, drainReason models.GPUPoolDrainReason) bool {
	switch state {
	case models.GPUPoolStateProvisioning, models.GPUPoolStateReady:
		return true
	case models.GPUPoolStateDraining:
		return drainReason == models.GPUPoolDrainReasonUserGrace
	default:
		return false
	}
}

// reconnectWindow is true when the user may Start again without a fresh startup charge.
func (s *gpuPoolService) reconnectWindow(pool *models.GPUPool, userID string) (eligible bool, until *time.Time, skipStartup bool) {
	if pool == nil || userID == "" {
		return false, nil, false
	}
	stopped := lastStoppedSession(pool, userID)
	if stopped == nil || stopped.StoppedAt == nil {
		return false, nil, false
	}
	if stopped.CreditsStartupCharged <= 0 || stopped.StartupRefunded {
		return false, nil, false
	}
	untilTime := stopped.StoppedAt.Add(s.reconnectCooldown())
	if time.Now().UTC().After(untilTime) {
		return false, nil, false
	}
	if !poolReusableForReconnect(pool.State, pool.DrainReason) {
		if pool.State != models.GPUPoolStateProvisioning || pool.RefCount != 0 {
			return false, nil, false
		}
	}
	return true, &untilTime, true
}

func (s *gpuPoolService) chargeSessionStartup(ctx context.Context, serviceTag, userID string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	sess := activeSession(pool, userID)
	if sess == nil {
		return nil
	}
	if sess.CreditsStartupCharged > 0 {
		return nil
	}

	amount := s.startupCredits()
	if eligible, _, skip := s.reconnectWindow(pool, userID); skip && eligible {
		_, err = s.poolRepo.UpdateActiveSession(ctx, serviceTag, userID, func(s *models.GPUPoolSession) {
			s.CreditsStartupCharged = amount
		})
		return err
	}

	if err := s.deductSessionCredits(ctx, userID, amount); err != nil {
		return err
	}

	_, err = s.poolRepo.UpdateActiveSession(ctx, serviceTag, userID, func(s *models.GPUPoolSession) {
		s.CreditsStartupCharged = amount
		s.CreditsCharged += amount
	})
	return err
}

func poolLiveForSession(state models.GPUPoolState) bool {
	return state == models.GPUPoolStateProvisioning || state == models.GPUPoolStateReady
}

func (s *gpuPoolService) reconcileUserSession(ctx context.Context, pool *models.GPUPool, userID string) (*models.GPUPool, error) {
	if pool == nil || userID == "" || poolLiveForSession(pool.State) {
		return pool, nil
	}
	if activeSession(pool, userID) == nil {
		return pool, nil
	}

	serviceTag := pool.ServiceTag

	if pool.State == models.GPUPoolStateFailed {
		if err := s.refundFailedPoolSessions(ctx, serviceTag); err != nil {
			return pool, err
		}
		return s.poolRepo.GetByServiceTag(ctx, serviceTag)
	}

	sess := activeSession(pool, userID)
	if sess != nil && sess.BillingStartedAt != nil {
		if err := s.finalizeBillingOnStop(ctx, serviceTag, userID); err != nil {
			if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrInsufficientCredits {
				s.cancelMeterTick(ctx, serviceTag, userID)
			} else {
				return pool, err
			}
		}
	} else {
		s.cancelMeterTick(ctx, serviceTag, userID)
	}

	if _, err := s.poolRepo.StopSessionWithReason(ctx, serviceTag, userID, models.GPUPoolSessionEndPoolNotLive); err != nil {
		return pool, err
	}
	if err := s.afterSessionStopped(ctx, serviceTag); err != nil {
		return pool, err
	}
	return s.poolRepo.GetByServiceTag(ctx, serviceTag)
}

func (s *gpuPoolService) refundFailedPoolSessions(ctx context.Context, serviceTag string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	if pool == nil || pool.State != models.GPUPoolStateFailed {
		return nil
	}

	for _, sess := range pool.Sessions {
		if sess.StoppedAt != nil {
			continue
		}
		if sess.BillingStartedAt != nil {
			continue
		}
		if sess.CreditsStartupCharged <= 0 || sess.StartupRefunded {
			continue
		}

		userID := sess.UserID
		amount := sess.CreditsStartupCharged
		if _, err := s.creditsSvc.AddCredits(ctx, &models.AddCreditsRequest{
			UserID: userID,
			Amount: amount,
		}); err != nil {
			return err
		}

		_, err = s.poolRepo.UpdateActiveSession(ctx, serviceTag, userID, func(s *models.GPUPoolSession) {
			s.StartupRefunded = true
		})
		if err != nil {
			return err
		}

		_, _ = s.poolRepo.StopSessionWithReason(ctx, serviceTag, userID, models.GPUPoolSessionEndProvisionFailed)
	}
	return s.afterSessionStopped(ctx, serviceTag)
}

func (s *gpuPoolService) meterTickPrefix(serviceTag, userID string) string {
	return fmt.Sprintf("gpu-pool:%s:meter:%s", serviceTag, userID)
}

func (s *gpuPoolService) meterTickKey(serviceTag, userID string, runAfter time.Time) string {
	return fmt.Sprintf("%s:%d", s.meterTickPrefix(serviceTag, userID), runAfter.UTC().Unix())
}

func activeSession(pool *models.GPUPool, userID string) *models.GPUPoolSession {
	for i := range pool.Sessions {
		if pool.Sessions[i].UserID == userID && pool.Sessions[i].StoppedAt == nil {
			return &pool.Sessions[i]
		}
	}
	return nil
}

func lastStoppedSession(pool *models.GPUPool, userID string) *models.GPUPoolSession {
	var latest *models.GPUPoolSession
	for i := range pool.Sessions {
		sess := &pool.Sessions[i]
		if sess.UserID != userID || sess.StoppedAt == nil {
			continue
		}
		if latest == nil || sess.StoppedAt.After(*latest.StoppedAt) {
			latest = sess
		}
	}
	return latest
}

func (s *gpuPoolService) sessionStatusFields(pool *models.GPUPool, userID string) (creditsCharged int, billingActive bool, endReason string) {
	if sess := activeSession(pool, userID); sess != nil {
		return sess.CreditsCharged, sess.BillingStartedAt != nil, ""
	}
	if sess := lastStoppedSession(pool, userID); sess != nil {
		return sess.CreditsCharged, false, sess.EndReason
	}
	return 0, false, ""
}

func (s *gpuPoolService) checkStartCredits(ctx context.Context, userID, serviceTag string) error {
	balance, err := s.creditsSvc.GetBalance(ctx, userID)
	if err != nil {
		return err
	}
	min := s.minStartCredits()
	pool, _ := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if eligible, _, _ := s.reconnectWindow(pool, userID); eligible {
		min = s.creditsPerMinute()
	}
	if balance.Credits < min {
		return apperrors.NewAppError(
			apperrors.ErrInsufficientCredits,
			400,
			fmt.Sprintf("Need at least %d credits to start GPU testing", min),
			fmt.Sprintf("balance=%d min=%d", balance.Credits, min),
		)
	}
	return nil
}

func (s *gpuPoolService) scheduleMeterTick(ctx context.Context, serviceTag, userID string, runAfter time.Time) error {
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolMeterTick, EnqueueJobOptions{
		IdempotencyKey: s.meterTickKey(serviceTag, userID, runAfter),
		RunAfter:       runAfter,
		Payload: map[string]any{
			"serviceTag": serviceTag,
			"userId":     userID,
		},
		MaxAttempts: 1,
	})
	return err
}

func (s *gpuPoolService) cancelMeterTick(ctx context.Context, serviceTag, userID string) {
	_, _ = s.jobSvc.CancelPendingMeterTicks(ctx, serviceTag, userID)
}

func (s *gpuPoolService) nextMeterTickRunAfter(sess *models.GPUPoolSession) time.Time {
	now := time.Now().UTC()
	interval := s.meterInterval()
	if sess == nil || sess.LastMeteredAt == nil {
		return now.Add(interval)
	}
	next := sess.LastMeteredAt.Add(interval)
	if next.Before(now) {
		return now
	}
	return next
}

func (s *gpuPoolService) ensureMeterTickScheduled(ctx context.Context, pool *models.GPUPool, userID string) error {
	if pool == nil || pool.State != models.GPUPoolStateReady || userID == "" {
		return nil
	}
	sess := activeSession(pool, userID)
	if sess == nil || sess.BillingStartedAt == nil {
		return nil
	}
	active, err := s.jobSvc.HasActiveMeterTick(ctx, pool.ServiceTag, userID)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	return s.scheduleMeterTick(ctx, pool.ServiceTag, userID, s.nextMeterTickRunAfter(sess))
}

func (s *gpuPoolService) ensureBillingStarted(ctx context.Context, pool *models.GPUPool, userID string) error {
	if pool == nil || pool.State != models.GPUPoolStateReady {
		return nil
	}
	sess := activeSession(pool, userID)
	if sess == nil || sess.BillingStartedAt != nil {
		return nil
	}

	now := time.Now().UTC()
	meterPrefix := s.meterTickPrefix(pool.ServiceTag, userID)
	_, err := s.poolRepo.UpdateActiveSession(ctx, pool.ServiceTag, userID, func(s *models.GPUPoolSession) {
		s.BillingStartedAt = &now
		s.LastMeteredAt = &now
		s.CreditsMeterID = meterPrefix
	})
	if err != nil {
		return err
	}
	return s.scheduleMeterTick(ctx, pool.ServiceTag, userID, now.Add(s.meterInterval()))
}

func (s *gpuPoolService) deductSessionCredits(ctx context.Context, userID string, amount int) error {
	if amount <= 0 {
		return nil
	}
	_, err := s.creditsSvc.DeductCredits(ctx, &models.DeductCreditsRequest{
		UserID: userID,
		Amount: amount,
	})
	return err
}

func (s *gpuPoolService) chargeInterval(ctx context.Context, serviceTag, userID string) error {
	amount := s.creditsPerMinute()
	if err := s.deductSessionCredits(ctx, userID, amount); err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err := s.poolRepo.UpdateActiveSession(ctx, serviceTag, userID, func(sess *models.GPUPoolSession) {
		sess.CreditsCharged += amount
		sess.LastMeteredAt = &now
	})
	return err
}

func (s *gpuPoolService) chargeFinalPartial(ctx context.Context, serviceTag, userID string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	sess := activeSession(pool, userID)
	if sess == nil || sess.BillingStartedAt == nil || sess.LastMeteredAt == nil {
		return nil
	}
	if time.Since(*sess.LastMeteredAt) < time.Second {
		return nil
	}
	return s.chargeInterval(ctx, serviceTag, userID)
}

func (s *gpuPoolService) finalizeBillingOnStop(ctx context.Context, serviceTag, userID string) error {
	s.cancelMeterTick(ctx, serviceTag, userID)
	return s.chargeFinalPartial(ctx, serviceTag, userID)
}

func (s *gpuPoolService) autoStopInsufficientCredits(ctx context.Context, serviceTag, userID string) error {
	s.cancelMeterTick(ctx, serviceTag, userID)
	_, err := s.poolRepo.StopSessionWithReason(ctx, serviceTag, userID, models.GPUPoolSessionEndInsufficientCredits)
	if err != nil {
		return err
	}
	return s.afterSessionStopped(ctx, serviceTag)
}

// HandleMeterTick is invoked by the worker on each billing interval.
func (s *gpuPoolService) HandleMeterTick(ctx context.Context, serviceTag, userID string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	if pool == nil {
		return nil
	}
	sess := activeSession(pool, userID)
	if sess == nil || sess.BillingStartedAt == nil {
		return nil
	}
	if pool.State != models.GPUPoolStateReady {
		s.cancelMeterTick(ctx, serviceTag, userID)
		return nil
	}

	if err := s.chargeInterval(ctx, serviceTag, userID); err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrInsufficientCredits {
			return s.autoStopInsufficientCredits(ctx, serviceTag, userID)
		}
		return err
	}

	return s.scheduleMeterTick(ctx, serviceTag, userID, time.Now().UTC().Add(s.meterInterval()))
}
