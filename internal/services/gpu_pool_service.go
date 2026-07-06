package services

import (
	"context"
	"fmt"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
)

type GPUPoolService interface {
	Start(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error)
	Stop(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error)
	GetStatus(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error)
	ListAdmin(ctx context.Context) ([]*models.GPUPool, error)
	AdminShutdown(ctx context.Context, serviceTag string, immediate bool) (*models.GPUPool, error)
	AdminCancelGrace(ctx context.Context, serviceTag string) (*models.GPUPool, error)
	ReconcileIdleWarmPools(ctx context.Context) error
	HandleMeterTick(ctx context.Context, serviceTag, userID string) error
	HandleProvisionFailed(ctx context.Context, serviceTag string) error
	OnProvisionSkippedNoSessions(ctx context.Context, serviceTag string) error
}

type gpuPoolService struct {
	poolRepo   repository.GPUPoolRepository
	jobSvc     JobService
	creditsSvc CreditsService
	cfg        *config.Config
}

func NewGPUPoolService(poolRepo repository.GPUPoolRepository, jobSvc JobService, creditsSvc CreditsService, cfg *config.Config) GPUPoolService {
	return &gpuPoolService{poolRepo: poolRepo, jobSvc: jobSvc, creditsSvc: creditsSvc, cfg: cfg}
}

func (s *gpuPoolService) resolveServiceName(serviceTag string) (string, error) {
	name, ok := models.ServiceTagToPipelineService[serviceTag]
	if !ok || name == "" {
		return "", apperrors.NewAppError(apperrors.ErrValidation, 400, "unsupported gpu pool service tag")
	}
	return name, nil
}

func (s *gpuPoolService) provisionKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:provision", serviceTag)
}

func (s *gpuPoolService) destroyKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:destroy", serviceTag)
}

func (s *gpuPoolService) graceDestroyKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:grace_destroy", serviceTag)
}

func (s *gpuPoolService) gracePeriod() time.Duration {
	minutes := s.cfg.Worker.GracePeriodMin
	if minutes <= 0 {
		minutes = 5
	}
	return time.Duration(minutes) * time.Minute
}

func (s *gpuPoolService) gracePeriodSec() int {
	sec := int(s.gracePeriod().Seconds())
	if sec <= 0 {
		return 300
	}
	return sec
}

func (s *gpuPoolService) destroyAtForPool(pool *models.GPUPool) *time.Time {
	if pool == nil || pool.State != models.GPUPoolStateDraining || !pool.HasScheduledGraceDestroy() {
		return nil
	}
	if pool.DrainStartedAt == nil {
		return nil
	}
	t := pool.DrainStartedAt.Add(s.gracePeriod())
	return &t
}

func (s *gpuPoolService) enqueueGraceDestroy(ctx context.Context, serviceTag, serviceName string) error {
	runAfter := time.Now().UTC().Add(s.gracePeriod())
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolGraceDestroy, EnqueueJobOptions{
		IdempotencyKey: s.graceDestroyKey(serviceTag),
		RunAfter:       runAfter,
		Payload: map[string]any{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
		},
		MaxAttempts: 3,
	})
	if err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrValidation {
			return nil
		}
		return err
	}
	return nil
}

func (s *gpuPoolService) adminDestroyInProgress(ctx context.Context, serviceTag string) (bool, error) {
	running, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag)
	if err != nil {
		return false, err
	}
	if running {
		return true, nil
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.destroyKey(serviceTag))
	return active, err
}

// maybeScheduleUserGraceDestroy runs after Stop when the last session ends on any warm node.
func (s *gpuPoolService) maybeScheduleUserGraceDestroy(ctx context.Context, pool *models.GPUPool, serviceName string) error {
	if pool == nil || pool.RefCount > 0 || !pool.TeardownOnUserStop() {
		return nil
	}
	if pool.AdminWarmHold {
		return nil
	}
	if pool.NodeID == "" && pool.PublicIP == "" {
		return nil
	}
	if destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag); err != nil {
		return err
	} else if destroying {
		return nil
	}

	now := time.Now().UTC()
	updated, err := s.poolRepo.Update(ctx, pool.ServiceTag, map[string]any{
		"state":          models.GPUPoolStateDraining,
		"drainStartedAt": now,
		"drainReason":    models.GPUPoolDrainReasonUserGrace,
	})
	if err != nil {
		return err
	}
	_ = updated
	return s.enqueueGraceDestroy(ctx, pool.ServiceTag, serviceName)
}

func (s *gpuPoolService) afterSessionStopped(ctx context.Context, serviceTag string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	if pool == nil || pool.RefCount > 0 {
		return nil
	}
	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil
	}
	return s.maybeScheduleUserGraceDestroy(ctx, pool, serviceName)
}

// reconcileIdleWarmPool schedules grace teardown for ready pools with no sessions but a live VM.
func (s *gpuPoolService) reconcileIdleWarmPool(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.RefCount > 0 || pool.State != models.GPUPoolStateReady {
		return pool, nil
	}
	if pool.AdminWarmHold {
		return pool, nil
	}
	if !pool.TeardownOnUserStop() {
		return pool, nil
	}
	serviceName, err := s.resolveServiceName(pool.ServiceTag)
	if err != nil {
		return pool, nil
	}
	if err := s.maybeScheduleUserGraceDestroy(ctx, pool, serviceName); err != nil {
		return pool, err
	}
	return s.poolRepo.GetByServiceTag(ctx, pool.ServiceTag)
}

func (s *gpuPoolService) ReconcileIdleWarmPools(ctx context.Context) error {
	pools, err := s.poolRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if pool == nil {
			continue
		}
		if _, err := s.reconcileIdleWarmPool(ctx, pool); err != nil {
			return err
		}
	}
	return nil
}

// cancelUserGraceIfResuming clears user-grace draining so Start Testing can reuse the node.
func (s *gpuPoolService) cancelUserGraceIfResuming(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateDraining {
		return pool, nil
	}
	destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag)
	if err != nil {
		return pool, err
	}
	if destroying {
		return pool, nil
	}
	if pool.DrainReason != models.GPUPoolDrainReasonUserGrace {
		return pool, nil
	}

	return s.cancelScheduledGrace(ctx, pool)
}

func (s *gpuPoolService) enqueueDestroy(ctx context.Context, serviceTag, serviceName string) error {
	key := s.destroyKey(serviceTag)
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolDestroy, EnqueueJobOptions{
		IdempotencyKey: key,
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
		running, runErr := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag)
		if runErr != nil {
			return runErr
		}
		if active || running {
			return nil
		}
	}
	return err
}

// adminAbortPoolNoNode cancels an in-flight boot when no E2E node exists yet (or metadata was lost).
func (s *gpuPoolService) adminAbortPoolNoNode(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.provisionKey(serviceTag))
	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))

	if _, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":          models.GPUPoolStateFailed,
		"lastError":      SanitizeGPUPoolUserError("admin terminated before GPU was ready"),
		"adminWarmHold":  false,
		"drainReason":    "",
		"drainStartedAt": nil,
	}); err != nil {
		return nil, err
	}
	_ = s.refundFailedPoolSessions(ctx, serviceTag)

	return s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":            models.GPUPoolStateIdle,
		"lastError":        "",
		"refCount":         0,
		"nodeId":           "",
		"publicIp":         "",
		"previousPublicIp": "",
		"readyAt":          nil,
	})
}

func (s *gpuPoolService) enqueueProvision(ctx context.Context, serviceTag, serviceName string) error {
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolProvision, EnqueueJobOptions{
		IdempotencyKey: s.provisionKey(serviceTag),
		Payload: map[string]any{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
		},
		MaxAttempts: 3,
	})
	if err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrValidation {
			return nil
		}
		return err
	}
	return nil
}

// ensureProvisionJob re-queues when pool is provisioning but the worker job was lost
// (e.g. dead idempotency row blocked a fresh enqueue).
func (s *gpuPoolService) ensureProvisionJob(ctx context.Context, pool *models.GPUPool) error {
	if pool == nil || pool.RefCount == 0 || pool.State != models.GPUPoolStateProvisioning {
		return nil
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(pool.ServiceTag))
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	return s.enqueueProvision(ctx, pool.ServiceTag, pool.ServiceName)
}

// reconcilePool re-enqueues destroy for draining pools stuck without an active job
// (e.g. worker restart mid-destroy). Never clears node metadata while a VM may still exist.
func (s *gpuPoolService) reconcilePool(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateDraining || pool.RefCount > 0 {
		return pool, nil
	}

	if pool.NodeID == "" && pool.PublicIP == "" {
		return s.poolRepo.Update(ctx, pool.ServiceTag, map[string]any{
			"state":            models.GPUPoolStateIdle,
			"nodeId":           "",
			"publicIp":         "",
			"previousPublicIp": "",
			"drainStartedAt":   nil,
			"drainReason":      "",
			"readyAt":          nil,
		})
	}

	const drainRetryAfter = 3 * time.Minute
	deadline := time.Now().UTC()
	if pool.DrainStartedAt != nil {
		deadline = pool.DrainStartedAt.Add(drainRetryAfter)
	}
	if time.Now().UTC().Before(deadline) {
		return pool, nil
	}

	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.destroyKey(pool.ServiceTag))
	if err != nil {
		return pool, err
	}
	running, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, pool.ServiceTag)
	if err != nil {
		return pool, err
	}
	if active || running {
		return pool, nil
	}

	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(pool.ServiceTag))
	if err := s.enqueueDestroy(ctx, pool.ServiceTag, pool.ServiceName); err != nil {
		return pool, err
	}
	return pool, nil
}

// reconcileStuckProvision marks provisioning pools failed when the worker job is gone or
// exceeded wall-clock limits (E2E Creating/Deleting stall, zombie running job).
// When refCount=0 (user stopped mid-provision), clears stuck provisioning so Start can retry.
func (s *gpuPoolService) reconcileStuckProvision(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateProvisioning {
		return pool, nil
	}

	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(pool.ServiceTag))
	if err != nil {
		return pool, err
	}

	if pool.RefCount == 0 {
		if active {
			return pool, nil
		}
		update := map[string]any{
			"state": models.GPUPoolStateIdle,
		}
		if pool.LastError != "" {
			update["state"] = models.GPUPoolStateFailed
		}
		return s.poolRepo.Update(ctx, pool.ServiceTag, update)
	}

	age := time.Since(pool.UpdatedAt)

	const (
		noJobFailAfter  = 13 * time.Minute // worker E2E_WAIT 10m + stall 6m headroom
		zombieFailAfter = 22 * time.Minute
	)
	if active && age < zombieFailAfter {
		return pool, nil
	}
	if !active && age < noJobFailAfter {
		return pool, nil
	}

	updated, err := s.poolRepo.Update(ctx, pool.ServiceTag, map[string]any{
		"state":     models.GPUPoolStateFailed,
		"lastError": SanitizeGPUPoolUserError("provision stalled beyond time limit"),
	})
	if err != nil {
		return pool, err
	}
	if updated.State == models.GPUPoolStateFailed {
		_ = s.refundFailedPoolSessions(ctx, pool.ServiceTag)
		updated, _ = s.poolRepo.GetByServiceTag(ctx, pool.ServiceTag)
	}
	return updated, nil
}

func (s *gpuPoolService) HandleProvisionFailed(ctx context.Context, serviceTag string) error {
	return s.refundFailedPoolSessions(ctx, serviceTag)
}

// OnProvisionSkippedNoSessions keeps warm nodes in grace or holds provisioning during reconnect cooldown.
func (s *gpuPoolService) OnProvisionSkippedNoSessions(ctx context.Context, serviceTag string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil || pool == nil || pool.RefCount > 0 {
		return err
	}
	if pool.TeardownOnUserStop() {
		if pool.State == models.GPUPoolStateDraining && pool.DrainReason == models.GPUPoolDrainReasonUserGrace {
			return nil
		}
		serviceName, svcErr := s.resolveServiceName(serviceTag)
		if svcErr != nil {
			return nil
		}
		return s.maybeScheduleUserGraceDestroy(ctx, pool, serviceName)
	}
	if pool.State != models.GPUPoolStateProvisioning {
		return nil
	}
	if time.Since(pool.UpdatedAt) < s.reconnectCooldown() {
		return nil
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(serviceTag))
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	update := map[string]any{"state": models.GPUPoolStateIdle}
	if pool.LastError != "" {
		update["state"] = models.GPUPoolStateFailed
	}
	_, err = s.poolRepo.Update(ctx, serviceTag, update)
	return err
}

type gpuPoolStatusOpts struct {
	freshSessionStart bool
}

// gpuPoolReattachedSession is true when the client is reconnecting to an existing session
// (status poll or idempotent Start), not immediately after creating a new session.
func gpuPoolReattachedSession(userActive bool, hasActiveSession bool, freshSessionStart bool) bool {
	return userActive && hasActiveSession && !freshSessionStart
}

func (s *gpuPoolService) toStatus(pool *models.GPUPool, userID string, opts ...gpuPoolStatusOpts) *models.GPUPoolStatusResponse {
	var o gpuPoolStatusOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	userActive := false
	var activeSess *models.GPUPoolSession
	for i := range pool.Sessions {
		if pool.Sessions[i].UserID == userID && pool.Sessions[i].StoppedAt == nil {
			userActive = true
			activeSess = &pool.Sessions[i]
			break
		}
	}
	creditsCharged, billingActive, endReason := s.sessionStatusFields(pool, userID)
	startupCharged, gpuTimeCharged := 0, 0
	if activeSess != nil {
		startupCharged = activeSess.CreditsStartupCharged
		if startupCharged > creditsCharged {
			startupCharged = creditsCharged
		}
		gpuTimeCharged = creditsCharged - startupCharged
	} else if stopped := lastStoppedSession(pool, userID); stopped != nil {
		startupCharged = stopped.CreditsStartupCharged
		if startupCharged > creditsCharged {
			startupCharged = creditsCharged
		}
		gpuTimeCharged = creditsCharged - startupCharged
	}
	interval := s.meterInterval()
	lastError := SanitizeGPUPoolUserError(pool.LastError)
	if !userActive && pool.RefCount == 0 &&
		(pool.State == models.GPUPoolStateFailed || pool.State == models.GPUPoolStateIdle) {
		lastError = ""
	}
	reconnectEligible, reconnectUntil, _ := s.reconnectWindow(pool, userID)
	return &models.GPUPoolStatusResponse{
		ServiceTag:            pool.ServiceTag,
		ServiceName:           pool.ServiceName,
		State:                 pool.State,
		RefCount:              pool.RefCount,
		PublicIP:              pool.PublicIP,
		NodeID:                pool.NodeID,
		ReadyAt:               pool.ReadyAt,
		DrainStartedAt:        pool.DrainStartedAt,
		DrainReason:           pool.DrainReason,
		DestroyAt:             s.destroyAtForPool(pool),
		GracePeriodSec:        s.gracePeriodSec(),
		LastError:             lastError,
		PollURL:               fmt.Sprintf("/api/v1/gpu-pool/status?serviceTag=%s", pool.ServiceTag),
		UserActive:            userActive,
		CreditsChargedSession:        creditsCharged,
		CreditsStartupChargedSession: startupCharged,
		CreditsGpuTimeSession:        gpuTimeCharged,
		CreditsPerMinute:             s.creditsPerMinute(),
		StartupCredits:        s.startupCredits(),
		MinCreditsToStart:     s.minStartCredits(),
		MeterIntervalSec:      s.meterIntervalSec(),
		NextMeterChargeAt:     nextMeterChargeAt(activeSess, interval),
		BillingActive:         billingActive,
		ReattachedSession:     gpuPoolReattachedSession(userActive, activeSess != nil, o.freshSessionStart),
		SessionEndReason:      endReason,
		ReconnectEligible:     reconnectEligible,
		ReconnectUntil:        reconnectUntil,
	}
}

func (s *gpuPoolService) Start(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	if userID == "" || serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "userId and serviceTag are required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}

	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	existing, _ := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if existing != nil {
		reconciled, err := s.reconcilePool(ctx, existing)
		if err != nil {
			return nil, err
		}
		reconciled, err = s.reconcileStuckProvision(ctx, reconciled)
		if err != nil {
			return nil, err
		}
		reconciled, err = s.reconcileUserSession(ctx, reconciled, userID)
		if err != nil {
			return nil, err
		}
		existing = reconciled
	}

	if existing != nil {
		resumed, err := s.cancelUserGraceIfResuming(ctx, existing)
		if err != nil {
			return nil, err
		}
		existing = resumed
	}

	graceKey := s.graceDestroyKey(serviceTag)
	if existing != nil && existing.State == models.GPUPoolStateDraining {
		msg := "The test GPU is shutting down. Contact an admin to turn it back on."
		if existing.DrainReason == models.GPUPoolDrainReasonAdminGrace {
			msg = "This test GPU is shutting down. Contact support."
		}
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, msg)
	}
	if existing != nil && existing.State == models.GPUPoolStateReady {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, graceKey)
	}

	if destroying, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag); err != nil {
		return nil, err
	} else if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409,
			"GPU is shutting down. Wait a minute and try Start Testing again.")
	}

	alreadyActive := existing != nil && activeSession(existing, userID) != nil
	if !alreadyActive {
		if err := s.checkStartCredits(ctx, userID, serviceTag); err != nil {
			return nil, err
		}
	}

	pool, provisionNeeded, err := s.poolRepo.StartSession(ctx, serviceTag, userID)
	if err != nil {
		return nil, err
	}

	if !alreadyActive {
		if err := s.chargeSessionStartup(ctx, serviceTag, userID); err != nil {
			_, _ = s.poolRepo.StopSession(ctx, serviceTag, userID)
			return nil, err
		}
	}

	if pool.State == models.GPUPoolStateReady {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, graceKey)
	}

	if provisionNeeded {
		if err := s.enqueueProvision(ctx, serviceTag, serviceName); err != nil {
			return nil, err
		}
	} else if pool.State == models.GPUPoolStateProvisioning && pool.RefCount > 0 {
		if err := s.ensureProvisionJob(ctx, pool); err != nil {
			return nil, err
		}
	}

	updated, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if err := s.ensureBillingStarted(ctx, updated, userID); err != nil {
		return nil, err
	}
	if err := s.ensureMeterTickScheduled(ctx, updated, userID); err != nil {
		return nil, err
	}
	updated, err = s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	return s.toStatus(updated, userID, gpuPoolStatusOpts{freshSessionStart: !alreadyActive}), nil
}

func (s *gpuPoolService) Stop(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	if userID == "" || serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "userId and serviceTag are required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}

	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	if err := s.finalizeBillingOnStop(ctx, serviceTag, userID); err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrInsufficientCredits {
			// Best-effort stop even if final partial minute cannot be paid.
			s.cancelMeterTick(ctx, serviceTag, userID)
		} else {
			return nil, err
		}
	}

	pool, err := s.poolRepo.StopSession(ctx, serviceTag, userID)
	if err != nil {
		return nil, err
	}

	if pool.RefCount == 0 {
		if err := s.afterSessionStopped(ctx, serviceTag); err != nil {
			return nil, err
		}
		pool, err = s.poolRepo.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
	}

	return s.toStatus(pool, userID), nil
}

// cancelScheduledGrace clears a grace teardown and returns the pool to ready (user Start or admin override).
func (s *gpuPoolService) cancelScheduledGrace(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateDraining {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "pool is not draining")
	}
	if !pool.HasScheduledGraceDestroy() {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "no scheduled grace teardown to cancel")
	}
	destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag)
	if err != nil {
		return nil, err
	}
	if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "immediate destroy already in progress")
	}

	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(pool.ServiceTag))
	return s.poolRepo.Update(ctx, pool.ServiceTag, map[string]any{
		"state":          models.GPUPoolStateReady,
		"drainStartedAt": nil,
		"drainReason":    "",
		"adminWarmHold":  true,
	})
}

func (s *gpuPoolService) AdminCancelGrace(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}
	if pool.State == models.GPUPoolStateDraining && pool.HasScheduledGraceDestroy() {
		return s.cancelScheduledGrace(ctx, pool)
	}
	if pool.State == models.GPUPoolStateReady && pool.TeardownOnUserStop() {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))
		return s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"adminWarmHold": true,
		})
	}
	return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "pool is not eligible for warm hold")
}

func (s *gpuPoolService) AdminShutdown(ctx context.Context, serviceTag string, immediate bool) (*models.GPUPool, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}

	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	if destroying, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag); err != nil {
		return nil, err
	} else if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "GPU shutdown already in progress")
	}

	poolBefore, _ := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if poolBefore != nil {
		for _, sess := range poolBefore.Sessions {
			if sess.StoppedAt == nil && sess.BillingStartedAt != nil {
				_ = s.finalizeBillingOnStop(ctx, serviceTag, sess.UserID)
			}
		}
	}

	if poolBefore != nil && poolBefore.NodeID == "" && poolBefore.PublicIP == "" {
		switch poolBefore.State {
		case models.GPUPoolStateProvisioning, models.GPUPoolStateFailed, models.GPUPoolStateDraining:
			return s.adminAbortPoolNoNode(ctx, serviceTag)
		}
	}

	// Pool already in grace drain — admin override without re-running session finalize.
	if poolBefore != nil && poolBefore.State == models.GPUPoolStateDraining && poolBefore.HasScheduledGraceDestroy() {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))
		if immediate {
			_, _ = s.poolRepo.Update(ctx, serviceTag, map[string]any{"adminWarmHold": false})
			if err := s.enqueueDestroy(ctx, serviceTag, serviceName); err != nil {
				return nil, err
			}
			return poolBefore, nil
		}
		now := time.Now().UTC()
		updated, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"drainReason":    models.GPUPoolDrainReasonAdminGrace,
			"drainStartedAt": now,
			"refCount":       0,
			"adminWarmHold":  false,
		})
		if err != nil {
			return nil, err
		}
		if err := s.enqueueGraceDestroy(ctx, serviceTag, serviceName); err != nil {
			return nil, err
		}
		return updated, nil
	}

	pool, err := s.poolRepo.AdminShutdown(ctx, serviceTag)
	if err != nil {
		return nil, err
	}

	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))

	_, _ = s.poolRepo.Update(ctx, serviceTag, map[string]any{"adminWarmHold": false})

	if immediate {
		if err := s.enqueueDestroy(ctx, serviceTag, serviceName); err != nil {
			return nil, err
		}
		return pool, nil
	}

	updated, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"drainReason": models.GPUPoolDrainReasonAdminGrace,
	})
	if err != nil {
		return nil, err
	}
	if err := s.enqueueGraceDestroy(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *gpuPoolService) GetStatus(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}

	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		serviceName, svcErr := s.resolveServiceName(serviceTag)
		if svcErr != nil {
			return nil, svcErr
		}
		pool, err = s.poolRepo.EnsurePool(ctx, serviceTag, serviceName)
		if err != nil {
			return nil, err
		}
	}
	reconciled, err := s.reconcilePool(ctx, pool)
	if err != nil {
		return nil, err
	}
	reconciled, err = s.reconcileIdleWarmPool(ctx, reconciled)
	if err != nil {
		return nil, err
	}
	reconciled, err = s.reconcileStuckProvision(ctx, reconciled)
	if err != nil {
		return nil, err
	}
	reconciled, err = s.reconcileUserSession(ctx, reconciled, userID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureBillingStarted(ctx, reconciled, userID); err != nil {
		return nil, err
	}
	if err := s.ensureMeterTickScheduled(ctx, reconciled, userID); err != nil {
		return nil, err
	}
	reconciled, err = s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	return s.toStatus(reconciled, userID), nil
}

func (s *gpuPoolService) ListAdmin(ctx context.Context) ([]*models.GPUPool, error) {
	pools, err := s.poolRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i, pool := range pools {
		if pool == nil {
			continue
		}
		reconciled, err := s.reconcileIdleWarmPool(ctx, pool)
		if err != nil {
			return nil, err
		}
		reconciled, err = s.reconcilePool(ctx, reconciled)
		if err != nil {
			return nil, err
		}
		pools[i] = reconciled
	}
	return pools, nil
}
