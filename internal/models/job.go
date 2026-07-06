package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusDead      JobStatus = "dead"
	JobStatusCancelled JobStatus = "cancelled"
)

type Job struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Type           string             `bson:"type" json:"type"`
	Status         JobStatus          `bson:"status" json:"status"`
	Payload        map[string]any     `bson:"payload" json:"payload"`
	RunAfter       time.Time          `bson:"runAfter" json:"runAfter"`
	Attempts       int                `bson:"attempts" json:"attempts"`
	MaxAttempts    int                `bson:"maxAttempts" json:"maxAttempts"`
	LockedBy       string             `bson:"lockedBy,omitempty" json:"lockedBy,omitempty"`
	LockedAt       *time.Time         `bson:"lockedAt,omitempty" json:"lockedAt,omitempty"`
	IdempotencyKey string             `bson:"idempotencyKey,omitempty" json:"idempotencyKey,omitempty"`
	CreatedAt      time.Time          `bson:"createdAt" json:"createdAt"`
	CompletedAt    *time.Time         `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	LastError      string             `bson:"lastError,omitempty" json:"lastError,omitempty"`
}

const (
	JobTypeSystemNoop         = "system.noop"
	JobTypeGPUPoolProvision   = "gpu.pool.provision"
	JobTypeGPUPoolDestroy     = "gpu.pool.destroy"
	JobTypeGPUPoolGraceDestroy = "gpu.pool.grace_destroy"
	JobTypeGPUPoolHealthCheck = "gpu.pool.health_check"
	JobTypeGPUPoolMeterTick   = "gpu.pool.meter_tick"
)
