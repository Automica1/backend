package services

import (
	"context"
	"fmt"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type EnqueueJobOptions struct {
	RunAfter       time.Time
	MaxAttempts    int
	IdempotencyKey string
	Payload        map[string]any
}

type JobService interface {
	Enqueue(ctx context.Context, jobType string, opts EnqueueJobOptions) (*models.Job, error)
	CancelPendingByIdempotencyKey(ctx context.Context, key string) (int64, error)
	ReschedulePendingByIdempotencyKey(ctx context.Context, key string, runAfter time.Time) (int64, error)
	CancelRunningByIdempotencyKey(ctx context.Context, key string) (int64, error)
	MarkCancelled(ctx context.Context, jobID primitive.ObjectID) error
	HasActiveByIdempotencyKey(ctx context.Context, key string) (bool, error)
	HasRunningGPUPoolDestroy(ctx context.Context, serviceTag string) (bool, error)
	HasActiveMeterTick(ctx context.Context, serviceTag, userID string) (bool, error)
	CancelPendingMeterTicks(ctx context.Context, serviceTag, userID string) (int64, error)
	MarkDeadByIdempotencyKey(ctx context.Context, key, reason string) (int64, error)
}

type jobService struct {
	repo repository.JobRepository
}

func NewJobService(repo repository.JobRepository) JobService {
	return &jobService{repo: repo}
}

func (s *jobService) Enqueue(ctx context.Context, jobType string, opts EnqueueJobOptions) (*models.Job, error) {
	if jobType == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "job type is required")
	}

	runAfter := opts.RunAfter
	if runAfter.IsZero() {
		runAfter = time.Now().UTC()
	}

	maxAttempts := opts.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}

	job := &models.Job{
		Type:           jobType,
		Status:         models.JobStatusPending,
		Payload:        opts.Payload,
		RunAfter:       runAfter,
		MaxAttempts:    maxAttempts,
		IdempotencyKey: opts.IdempotencyKey,
		CreatedAt:      time.Now().UTC(),
	}

	if err := s.repo.Insert(ctx, job); err != nil {
		if mongo.IsDuplicateKeyError(err) && opts.IdempotencyKey != "" {
			reactivated, reactErr := s.repo.ReactivateByIdempotencyKey(
				ctx, opts.IdempotencyKey, jobType, opts.Payload, runAfter, maxAttempts,
			)
			if reactErr != nil {
				return nil, reactErr
			}
			if reactivated != nil {
				return reactivated, nil
			}
			return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, fmt.Sprintf("job already queued: %s", opts.IdempotencyKey))
		}
		return nil, err
	}
	return job, nil
}

func (s *jobService) HasActiveByIdempotencyKey(ctx context.Context, key string) (bool, error) {
	return s.repo.HasActiveByIdempotencyKey(ctx, key)
}

func (s *jobService) HasRunningGPUPoolDestroy(ctx context.Context, serviceTag string) (bool, error) {
	return s.repo.HasRunningGPUPoolDestroy(ctx, serviceTag)
}

func (s *jobService) CancelPendingByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	return s.repo.CancelPendingByIdempotencyKey(ctx, key)
}

func (s *jobService) ReschedulePendingByIdempotencyKey(ctx context.Context, key string, runAfter time.Time) (int64, error) {
	return s.repo.ReschedulePendingByIdempotencyKey(ctx, key, runAfter)
}

func (s *jobService) CancelRunningByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	return s.repo.CancelRunningByIdempotencyKey(ctx, key)
}

func (s *jobService) MarkCancelled(ctx context.Context, jobID primitive.ObjectID) error {
	return s.repo.MarkCancelled(ctx, jobID)
}

func (s *jobService) HasActiveMeterTick(ctx context.Context, serviceTag, userID string) (bool, error) {
	return s.repo.HasActiveMeterTick(ctx, serviceTag, userID)
}

func (s *jobService) CancelPendingMeterTicks(ctx context.Context, serviceTag, userID string) (int64, error) {
	return s.repo.CancelPendingMeterTicks(ctx, serviceTag, userID)
}

func (s *jobService) MarkDeadByIdempotencyKey(ctx context.Context, key, reason string) (int64, error) {
	return s.repo.MarkDeadByIdempotencyKey(ctx, key, reason)
}
