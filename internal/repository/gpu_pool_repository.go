package repository

import (
	"context"
	"errors"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type GPUPoolRepository interface {
	GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error)
	EnsurePool(ctx context.Context, serviceTag, serviceName string) (*models.GPUPool, error)
	Update(ctx context.Context, serviceTag string, update bson.M) (*models.GPUPool, error)
	List(ctx context.Context) ([]*models.GPUPool, error)
	StartSession(ctx context.Context, serviceTag, userID string) (*models.GPUPool, bool, error)
	StopSession(ctx context.Context, serviceTag, userID string) (*models.GPUPool, error)
	StopSessionWithReason(ctx context.Context, serviceTag, userID, endReason string) (*models.GPUPool, error)
	UpdateActiveSession(ctx context.Context, serviceTag, userID string, update func(*models.GPUPoolSession)) (*models.GPUPool, error)
	AdminShutdown(ctx context.Context, serviceTag string) (*models.GPUPool, error)
}

type gpuPoolRepository struct {
	collection *mongo.Collection
}

func NewGPUPoolRepository(collection *mongo.Collection) GPUPoolRepository {
	return &gpuPoolRepository{collection: collection}
}

func (r *gpuPoolRepository) GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	var pool models.GPUPool
	err := r.collection.FindOne(ctx, bson.M{"serviceTag": serviceTag}).Decode(&pool)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &pool, nil
}

func (r *gpuPoolRepository) EnsurePool(ctx context.Context, serviceTag, serviceName string) (*models.GPUPool, error) {
	now := time.Now().UTC()
	filter := bson.M{"serviceTag": serviceTag}
	update := bson.M{
		"$setOnInsert": bson.M{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
			"state":       models.GPUPoolStateIdle,
			"refCount":    0,
			"sessions":    []models.GPUPoolSession{},
			"createdAt":   now,
		},
		"$set": bson.M{"updatedAt": now},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var pool models.GPUPool
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&pool)
	if err != nil {
		return nil, err
	}
	return &pool, nil
}

func (r *gpuPoolRepository) Update(ctx context.Context, serviceTag string, update bson.M) (*models.GPUPool, error) {
	update["updatedAt"] = time.Now().UTC()
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var pool models.GPUPool
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"serviceTag": serviceTag}, bson.M{"$set": update}, opts).Decode(&pool)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
		}
		return nil, err
	}
	return &pool, nil
}

func (r *gpuPoolRepository) List(ctx context.Context) ([]*models.GPUPool, error) {
	cursor, err := r.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "serviceTag", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var pools []*models.GPUPool
	for cursor.Next(ctx) {
		var pool models.GPUPool
		if err := cursor.Decode(&pool); err != nil {
			return nil, err
		}
		pools = append(pools, &pool)
	}
	return pools, cursor.Err()
}

func activeSessionIndex(pool *models.GPUPool, userID string) int {
	for i, s := range pool.Sessions {
		if s.UserID == userID && s.StoppedAt == nil {
			return i
		}
	}
	return -1
}

func (r *gpuPoolRepository) StartSession(ctx context.Context, serviceTag, userID string) (*models.GPUPool, bool, error) {
	now := time.Now().UTC()
	provisionNeeded := false

	for attempt := 0; attempt < 5; attempt++ {
		pool, err := r.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, false, err
		}
		if pool == nil {
			return nil, false, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
		}

		if idx := activeSessionIndex(pool, userID); idx >= 0 {
			return pool, false, nil
		}

		session := models.GPUPoolSession{UserID: userID, StartedAt: now}
		newSessions := append(pool.Sessions, session)
		newRef := pool.RefCount + 1

		update := bson.M{
			"refCount": newRef,
			"sessions": newSessions,
		}

		switch pool.State {
		case models.GPUPoolStateIdle, models.GPUPoolStateFailed:
			update["state"] = models.GPUPoolStateProvisioning
			update["lastError"] = ""
			update["nodeOwner"] = models.GPUPoolNodeOwnerUser
			provisionNeeded = true
		case models.GPUPoolStateDraining:
			return nil, false, apperrors.NewAppError(apperrors.ErrValidation, 409, "gpu pool is shutting down")
		case models.GPUPoolStateReady, models.GPUPoolStateProvisioning:
			// no state change
		default:
			update["state"] = models.GPUPoolStateProvisioning
			update["nodeOwner"] = models.GPUPoolNodeOwnerUser
			provisionNeeded = true
		}

		filter := bson.M{
			"serviceTag": serviceTag,
			"refCount":   pool.RefCount,
			"state":      pool.State,
		}

		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var updated models.GPUPool
		err = r.collection.FindOneAndUpdate(ctx, filter, bson.M{"$set": update}, opts).Decode(&updated)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, false, err
		}
		return &updated, provisionNeeded, nil
	}

	return nil, false, apperrors.NewAppError(apperrors.ErrValidation, 409, "gpu pool busy, retry start")
}

func (r *gpuPoolRepository) StopSession(ctx context.Context, serviceTag, userID string) (*models.GPUPool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		pool, err := r.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
		if pool == nil {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
		}

		idx := activeSessionIndex(pool, userID)
		if idx < 0 {
			return pool, nil
		}

		now := time.Now().UTC()
		sessions := make([]models.GPUPoolSession, len(pool.Sessions))
		copy(sessions, pool.Sessions)
		stopped := sessions[idx].StoppedAt
		if stopped == nil {
			t := now
			sessions[idx].StoppedAt = &t
		}

		newRef := pool.RefCount - 1
		if newRef < 0 {
			newRef = 0
		}

		update := bson.M{
			"refCount": newRef,
			"sessions": sessions,
		}
		// User Stop ends the session only — never change pool state (admin owns shutdown).

		filter := bson.M{
			"serviceTag": serviceTag,
			"refCount":   pool.RefCount,
		}

		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var updated models.GPUPool
		err = r.collection.FindOneAndUpdate(ctx, filter, bson.M{"$set": update}, opts).Decode(&updated)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, err
		}
		return &updated, nil
	}
	return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "gpu pool busy, retry stop")
}

func (r *gpuPoolRepository) StopSessionWithReason(ctx context.Context, serviceTag, userID, endReason string) (*models.GPUPool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		pool, err := r.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
		if pool == nil {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
		}

		idx := activeSessionIndex(pool, userID)
		if idx < 0 {
			return pool, nil
		}

		now := time.Now().UTC()
		sessions := make([]models.GPUPoolSession, len(pool.Sessions))
		copy(sessions, pool.Sessions)
		if sessions[idx].StoppedAt == nil {
			t := now
			sessions[idx].StoppedAt = &t
		}
		if endReason != "" {
			sessions[idx].EndReason = endReason
		}

		newRef := pool.RefCount - 1
		if newRef < 0 {
			newRef = 0
		}

		update := bson.M{
			"refCount": newRef,
			"sessions": sessions,
		}

		filter := bson.M{
			"serviceTag": serviceTag,
			"refCount":   pool.RefCount,
		}

		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var updated models.GPUPool
		err = r.collection.FindOneAndUpdate(ctx, filter, bson.M{"$set": update}, opts).Decode(&updated)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, err
		}
		return &updated, nil
	}
	return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "gpu pool busy, retry stop")
}

func (r *gpuPoolRepository) UpdateActiveSession(ctx context.Context, serviceTag, userID string, update func(*models.GPUPoolSession)) (*models.GPUPool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		pool, err := r.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
		if pool == nil {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
		}

		idx := activeSessionIndex(pool, userID)
		if idx < 0 {
			return pool, nil
		}

		sessions := make([]models.GPUPoolSession, len(pool.Sessions))
		copy(sessions, pool.Sessions)
		update(&sessions[idx])

		filter := bson.M{
			"serviceTag": serviceTag,
			"refCount":   pool.RefCount,
		}

		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var updated models.GPUPool
		err = r.collection.FindOneAndUpdate(ctx, filter, bson.M{
			"$set": bson.M{
				"sessions":  sessions,
				"updatedAt": time.Now().UTC(),
			},
		}, opts).Decode(&updated)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, err
		}
		return &updated, nil
	}
	return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "gpu pool busy, retry session update")
}

func (r *gpuPoolRepository) AdminShutdown(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	for attempt := 0; attempt < 5; attempt++ {
		pool, err := r.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
		if pool == nil {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
		}
		if pool.PublicIP == "" && pool.NodeID == "" {
			return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "no GPU node is running for this pool")
		}

		now := time.Now().UTC()
		sessions := make([]models.GPUPoolSession, len(pool.Sessions))
		copy(sessions, pool.Sessions)
		for i := range sessions {
			if sessions[i].StoppedAt == nil {
				t := now
				sessions[i].StoppedAt = &t
			}
		}

		update := bson.M{
			"refCount":       0,
			"sessions":       sessions,
			"state":          models.GPUPoolStateDraining,
			"drainStartedAt": now,
		}

		filter := bson.M{
			"serviceTag": serviceTag,
			"refCount":   pool.RefCount,
			"state":      pool.State,
		}

		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var updated models.GPUPool
		err = r.collection.FindOneAndUpdate(ctx, filter, bson.M{"$set": update}, opts).Decode(&updated)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, err
		}
		return &updated, nil
	}
	return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "gpu pool busy, retry shutdown")
}
