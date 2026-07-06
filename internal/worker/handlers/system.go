package handlers

import (
	"context"
	"log"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/worker"
)

func RegisterSystemHandlers(registry *worker.Registry) {
	registry.Register(models.JobTypeSystemNoop, handleSystemNoop)
}

func handleSystemNoop(ctx context.Context, job *models.Job) error {
	log.Printf("system.noop job=%s payload=%v", job.ID.Hex(), job.Payload)
	return nil
}
