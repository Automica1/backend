package services

import (
	"context"
	"testing"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type recordingJobService struct {
	cancelPendingKey  string
	cancelRunningKey  string
	destroyEnqueued   bool
	destroyServiceTag string
}

func (r *recordingJobService) Enqueue(ctx context.Context, jobType string, opts EnqueueJobOptions) (*models.Job, error) {
	if jobType == models.JobTypeGPUPoolDestroy {
		r.destroyEnqueued = true
		if tag, ok := opts.Payload["serviceTag"].(string); ok {
			r.destroyServiceTag = tag
		}
	}
	return &models.Job{ID: primitive.NewObjectID()}, nil
}

func (r *recordingJobService) CancelPendingByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	r.cancelPendingKey = key
	return 1, nil
}

func (r *recordingJobService) CancelRunningByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	r.cancelRunningKey = key
	return 1, nil
}

func (r *recordingJobService) MarkCancelled(ctx context.Context, jobID primitive.ObjectID) error {
	return nil
}

func (r *recordingJobService) HasActiveByIdempotencyKey(ctx context.Context, key string) (bool, error) {
	return false, nil
}

func (r *recordingJobService) HasRunningGPUPoolDestroy(ctx context.Context, serviceTag string) (bool, error) {
	return false, nil
}

func (r *recordingJobService) HasActiveMeterTick(ctx context.Context, serviceTag, userID string) (bool, error) {
	return false, nil
}

func (r *recordingJobService) CancelPendingMeterTicks(ctx context.Context, serviceTag, userID string) (int64, error) {
	return 0, nil
}

func TestAbortProvisionAndDestroy(t *testing.T) {
	t.Parallel()
	jobs := &recordingJobService{}
	svc := &gpuPoolService{
		cfg:    &config.Config{},
		jobSvc: jobs,
	}
	tag := "sign-verify-vlm"
	if err := svc.abortProvisionAndDestroy(context.Background(), tag, "sign_verify_vlm_gpu"); err != nil {
		t.Fatalf("abortProvisionAndDestroy: %v", err)
	}
	wantKey := "gpu-pool:" + tag + ":provision"
	if jobs.cancelPendingKey != wantKey || jobs.cancelRunningKey != wantKey {
		t.Fatalf("expected provision cancel keys %q, got pending=%q running=%q", wantKey, jobs.cancelPendingKey, jobs.cancelRunningKey)
	}
	if !jobs.destroyEnqueued || jobs.destroyServiceTag != tag {
		t.Fatalf("expected destroy enqueued for %s, got enqueued=%v tag=%q", tag, jobs.destroyEnqueued, jobs.destroyServiceTag)
	}
}

func TestAfterEarlyStopDuringProvisionNoNodeAlwaysAborts(t *testing.T) {
	t.Parallel()
	jobs := &recordingJobService{}
	svc := &gpuPoolService{
		cfg:    &config.Config{},
		jobSvc: jobs,
	}
	pool := &models.GPUPool{ServiceTag: "sign-verify-vlm", RefCount: 0}
	if err := svc.afterEarlyStopDuringProvision(context.Background(), pool, "sign_verify_vlm_gpu"); err != nil {
		t.Fatalf("afterEarlyStopDuringProvision: %v", err)
	}
	if jobs.cancelRunningKey == "" || !jobs.destroyEnqueued {
		t.Fatalf("expected abort with running cancel and destroy, got running=%q destroy=%v", jobs.cancelRunningKey, jobs.destroyEnqueued)
	}
}
