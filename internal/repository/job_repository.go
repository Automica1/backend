package repository

import (
	"context"
	"errors"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type JobRepository interface {
	Insert(ctx context.Context, job *models.Job) error
	GetByIdempotencyKey(ctx context.Context, key string) (*models.Job, error)
	HasActiveByIdempotencyKey(ctx context.Context, key string) (bool, error)
	HasRunningGPUPoolDestroy(ctx context.Context, serviceTag string) (bool, error)
	ReactivateByIdempotencyKey(ctx context.Context, key string, jobType string, payload map[string]any, runAfter time.Time, maxAttempts int) (*models.Job, error)
	ClaimNext(ctx context.Context, workerID string, now time.Time) (*models.Job, error)
	MarkCompleted(ctx context.Context, id primitive.ObjectID) error
	MarkFailed(ctx context.Context, id primitive.ObjectID, errMsg string, retry bool, runAfter time.Time) error
	MarkDead(ctx context.Context, id primitive.ObjectID, errMsg string) error
	ReleaseStaleLocks(ctx context.Context, workerID string, staleBefore time.Time) (int64, error)
	ReleaseAllRunningLocks(ctx context.Context, reason string) (int64, error)
	CancelPendingByIdempotencyKey(ctx context.Context, key string) (int64, error)
	CancelRunningByIdempotencyKey(ctx context.Context, key string) (int64, error)
	MarkCancelled(ctx context.Context, id primitive.ObjectID) error
	MarkDeadByIdempotencyKey(ctx context.Context, key, reason string) (int64, error)
	HasActiveMeterTick(ctx context.Context, serviceTag, userID string) (bool, error)
	CancelPendingMeterTicks(ctx context.Context, serviceTag, userID string) (int64, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*models.Job, error)
	ListGPUByServiceTag(ctx context.Context, serviceTag string, limit int) ([]*models.Job, error)
}

type jobRepository struct {
	collection *mongo.Collection
}

func NewJobRepository(collection *mongo.Collection) JobRepository {
	return &jobRepository{collection: collection}
}

func (r *jobRepository) Insert(ctx context.Context, job *models.Job) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.RunAfter.IsZero() {
		job.RunAfter = job.CreatedAt
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	if job.Status == "" {
		job.Status = models.JobStatusPending
	}
	result, err := r.collection.InsertOne(ctx, job)
	if err != nil {
		return err
	}
	job.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *jobRepository) GetByIdempotencyKey(ctx context.Context, key string) (*models.Job, error) {
	if key == "" {
		return nil, nil
	}
	var job models.Job
	err := r.collection.FindOne(ctx, bson.M{"idempotencyKey": key}).Decode(&job)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) HasActiveByIdempotencyKey(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"idempotencyKey": key,
		"status": bson.M{"$in": []models.JobStatus{
			models.JobStatusPending,
			models.JobStatusRunning,
		}},
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *jobRepository) HasRunningGPUPoolDestroy(ctx context.Context, serviceTag string) (bool, error) {
	if serviceTag == "" {
		return false, nil
	}
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"type": bson.M{"$in": []string{
			models.JobTypeGPUPoolGraceDestroy,
			models.JobTypeGPUPoolDestroy,
		}},
		"status":             models.JobStatusRunning,
		"payload.serviceTag": serviceTag,
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *jobRepository) ReactivateByIdempotencyKey(ctx context.Context, key string, jobType string, payload map[string]any, runAfter time.Time, maxAttempts int) (*models.Job, error) {
	if key == "" {
		return nil, errors.New("idempotency key required")
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if runAfter.IsZero() {
		runAfter = time.Now().UTC()
	}

	filter := bson.M{
		"idempotencyKey": key,
		"status": bson.M{"$in": []models.JobStatus{
			models.JobStatusDead,
			models.JobStatusCompleted,
			models.JobStatusCancelled,
		}},
	}
	update := bson.M{
		"$set": bson.M{
			"type":        jobType,
			"status":      models.JobStatusPending,
			"payload":     payload,
			"runAfter":    runAfter,
			"maxAttempts": maxAttempts,
			"attempts":    0,
			"lastError":   "",
			"lockedBy":    "",
			"lockedAt":    nil,
			"completedAt": nil,
		},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var job models.Job
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&job)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) ClaimNext(ctx context.Context, workerID string, now time.Time) (*models.Job, error) {
	filter := bson.M{
		"status":   models.JobStatusPending,
		"runAfter": bson.M{"$lte": now},
	}
	update := bson.M{
		"$set": bson.M{
			"status":   models.JobStatusRunning,
			"lockedBy": workerID,
			"lockedAt": now,
		},
		"$inc": bson.M{"attempts": 1},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After).SetSort(bson.D{
		{Key: "runAfter", Value: 1},
		{Key: "createdAt", Value: 1},
	})

	var job models.Job
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&job)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) MarkCompleted(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusCompleted,
			"completedAt": now,
			"lastError":   "",
		},
	})
	return err
}

func (r *jobRepository) MarkFailed(ctx context.Context, id primitive.ObjectID, errMsg string, retry bool, runAfter time.Time) error {
	if retry {
		_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$set": bson.M{
				"status":    models.JobStatusPending,
				"lastError": errMsg,
				"runAfter":  runAfter,
				"lockedBy":  "",
				"lockedAt":  nil,
			},
		})
		return err
	}
	return r.MarkDead(ctx, id, errMsg)
}

func (r *jobRepository) MarkDead(ctx context.Context, id primitive.ObjectID, errMsg string) error {
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusDead,
			"completedAt": now,
			"lastError":   errMsg,
			"lockedBy":    "",
			"lockedAt":    nil,
		},
	})
	return err
}

func (r *jobRepository) ReleaseAllRunningLocks(ctx context.Context, reason string) (int64, error) {
	result, err := r.collection.UpdateMany(ctx, bson.M{
		"status": models.JobStatusRunning,
	}, bson.M{
		"$set": bson.M{
			"status":    models.JobStatusPending,
			"lockedBy":  "",
			"lockedAt":  nil,
			"lastError": reason,
		},
	})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (r *jobRepository) ReleaseStaleLocks(ctx context.Context, workerID string, staleBefore time.Time) (int64, error) {
	// Reclaim any running job past lock timeout (including orphaned locks from dead workers).
	result, err := r.collection.UpdateMany(ctx, bson.M{
		"status": models.JobStatusRunning,
		"lockedAt": bson.M{"$lt": staleBefore},
	}, bson.M{
		"$set": bson.M{
			"status":    models.JobStatusPending,
			"lockedBy":  "",
			"lockedAt":  nil,
			"lastError": "lock released after worker timeout",
		},
	})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (r *jobRepository) CancelPendingByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	if key == "" {
		return 0, nil
	}
	now := time.Now().UTC()
	result, err := r.collection.UpdateMany(ctx, bson.M{
		"idempotencyKey": key,
		"status":         models.JobStatusPending,
	}, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusCancelled,
			"completedAt": now,
		},
	})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (r *jobRepository) CancelRunningByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	if key == "" {
		return 0, nil
	}
	now := time.Now().UTC()
	result, err := r.collection.UpdateMany(ctx, bson.M{
		"idempotencyKey": key,
		"status":         models.JobStatusRunning,
	}, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusCancelled,
			"completedAt": now,
			"lockedBy":    "",
			"lockedAt":    nil,
		},
	})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (r *jobRepository) MarkCancelled(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusCancelled,
			"completedAt": now,
			"lockedBy":    "",
			"lockedAt":    nil,
		},
	})
	return err
}

func meterTickJobFilter(serviceTag, userID string) bson.M {
	return bson.M{
		"type":               models.JobTypeGPUPoolMeterTick,
		"payload.serviceTag": serviceTag,
		"payload.userId":     userID,
	}
}

func (r *jobRepository) HasActiveMeterTick(ctx context.Context, serviceTag, userID string) (bool, error) {
	if serviceTag == "" || userID == "" {
		return false, nil
	}
	filter := meterTickJobFilter(serviceTag, userID)
	filter["status"] = bson.M{"$in": []models.JobStatus{
		models.JobStatusPending,
		models.JobStatusRunning,
	}}
	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *jobRepository) CancelPendingMeterTicks(ctx context.Context, serviceTag, userID string) (int64, error) {
	if serviceTag == "" || userID == "" {
		return 0, nil
	}
	now := time.Now().UTC()
	filter := meterTickJobFilter(serviceTag, userID)
	filter["status"] = models.JobStatusPending
	result, err := r.collection.UpdateMany(ctx, filter, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusCancelled,
			"completedAt": now,
		},
	})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (r *jobRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.Job, error) {
	var job models.Job
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&job)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}

func (r *jobRepository) ListGPUByServiceTag(ctx context.Context, serviceTag string, limit int) ([]*models.Job, error) {
	if serviceTag == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	filter := bson.M{
		"type": bson.M{"$in": []string{
			models.JobTypeGPUPoolProvision,
			models.JobTypeGPUPoolDestroy,
			models.JobTypeGPUPoolGraceDestroy,
		}},
		"payload.serviceTag": serviceTag,
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit))
	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var jobs []*models.Job
	for cur.Next(ctx) {
		var job models.Job
		if err := cur.Decode(&job); err != nil {
			return nil, err
		}
		j := job
		jobs = append(jobs, &j)
	}
	return jobs, cur.Err()
}

func (r *jobRepository) MarkDeadByIdempotencyKey(ctx context.Context, key, reason string) (int64, error) {
	if key == "" {
		return 0, nil
	}
	now := time.Now().UTC()
	result, err := r.collection.UpdateMany(ctx, bson.M{
		"idempotencyKey": key,
		"status": bson.M{"$in": []models.JobStatus{
			models.JobStatusPending,
			models.JobStatusRunning,
			models.JobStatusFailed,
		}},
	}, bson.M{
		"$set": bson.M{
			"status":      models.JobStatusDead,
			"lastError":   reason,
			"completedAt": now,
			"lockedBy":    "",
			"lockedAt":    nil,
		},
	})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}
