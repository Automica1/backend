package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
)

type Handler func(ctx context.Context, job *models.Job) error

type Registry struct {
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

func (r *Registry) Register(jobType string, handler Handler) {
	r.handlers[jobType] = handler
}

func (r *Registry) Get(jobType string) (Handler, bool) {
	h, ok := r.handlers[jobType]
	return h, ok
}

type Worker struct {
	cfg      *config.Config
	repo     repository.JobRepository
	registry *Registry
	workerID string
	stopCh   chan struct{}
}

func New(cfg *config.Config, repo repository.JobRepository, registry *Registry) *Worker {
	hostname, _ := os.Hostname()
	workerID := fmt.Sprintf("%s-%d", hostname, os.Getpid())
	return &Worker{
		cfg:      cfg,
		repo:     repo,
		registry: registry,
		workerID: workerID,
		stopCh:   make(chan struct{}),
	}
}

func (w *Worker) WorkerID() string {
	return w.workerID
}

func (w *Worker) Stop() {
	close(w.stopCh)
}

func (w *Worker) Run(ctx context.Context) {
	poll := time.Duration(w.cfg.Worker.PollIntervalSec) * time.Second
	if poll <= 0 {
		poll = 5 * time.Second
	}
	lockTimeout := time.Duration(w.cfg.Worker.LockTimeoutSec) * time.Second
	if lockTimeout <= 0 {
		lockTimeout = 30 * time.Minute
	}

	log.Printf("automica-worker started id=%s poll=%s", w.workerID, poll)

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("automica-worker context cancelled")
			return
		case <-w.stopCh:
			log.Println("automica-worker stop requested")
			return
		default:
			w.releaseStaleLocks(ctx, lockTimeout)
			if w.processOne(ctx) {
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) releaseStaleLocks(ctx context.Context, lockTimeout time.Duration) {
	staleBefore := time.Now().UTC().Add(-lockTimeout)
	count, err := w.repo.ReleaseStaleLocks(ctx, w.workerID, staleBefore)
	if err != nil {
		log.Printf("release stale locks error: %v", err)
		return
	}
	if count > 0 {
		log.Printf("released %d stale job lock(s)", count)
	}
}

func (w *Worker) processOne(ctx context.Context) bool {
	job, err := w.repo.ClaimNext(ctx, w.workerID, time.Now().UTC())
	if err != nil {
		log.Printf("claim job error: %v", err)
		return false
	}
	if job == nil {
		return false
	}

	log.Printf("running job id=%s type=%s attempt=%d/%d", job.ID.Hex(), job.Type, job.Attempts, job.MaxAttempts)

	handler, ok := w.registry.Get(job.Type)
	if !ok {
		_ = w.repo.MarkDead(ctx, job.ID, fmt.Sprintf("no handler for job type %s", job.Type))
		return true
	}

	runCtx := ctx
	if w.cfg.Worker.JobTimeoutSec > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(w.cfg.Worker.JobTimeoutSec)*time.Second)
		defer cancel()
	}

	err = handler(runCtx, job)
	markCtx := context.Background()
	if err == nil {
		if markErr := w.repo.MarkCompleted(markCtx, job.ID); markErr != nil {
			log.Printf("mark completed error: %v", markErr)
		}
		log.Printf("job completed id=%s type=%s", job.ID.Hex(), job.Type)
		return true
	}

	if IsJobCancelled(err) {
		if markErr := w.repo.MarkCancelled(markCtx, job.ID); markErr != nil {
			log.Printf("mark cancelled error: %v", markErr)
		}
		log.Printf("job cancelled id=%s type=%s", job.ID.Hex(), job.Type)
		return true
	}

	log.Printf("job failed id=%s type=%s err=%v", job.ID.Hex(), job.Type, err)
	retry := job.Attempts < job.MaxAttempts
	backoff := time.Duration(job.Attempts*job.Attempts) * 30 * time.Second
	if backoff < 30*time.Second {
		backoff = 30 * time.Second
	}
	runAfter := time.Now().UTC().Add(backoff)
	if markErr := w.repo.MarkFailed(markCtx, job.ID, err.Error(), retry, runAfter); markErr != nil {
		log.Printf("mark failed error: %v", markErr)
	}
	return true
}
