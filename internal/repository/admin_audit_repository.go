package repository

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type adminAuditRepository struct {
	collection *mongo.Collection
}

func NewAdminAuditRepository(collection *mongo.Collection) AdminAuditRepository {
	return &adminAuditRepository{collection: collection}
}

func (r *adminAuditRepository) Create(ctx context.Context, logEntry *models.AdminAuditLog) error {
	if logEntry.Timestamp.IsZero() {
		logEntry.Timestamp = time.Now()
	}
	_, err := r.collection.InsertOne(ctx, logEntry)
	return err
}

func (r *adminAuditRepository) GetRecent(ctx context.Context, limit int) ([]models.AdminAuditLog, error) {
	if limit <= 0 {
		limit = 20
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []models.AdminAuditLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *adminAuditRepository) GetRecentPaged(ctx context.Context, limit, skip int) ([]models.AdminAuditLog, error) {
	if limit <= 0 {
		limit = 20
	}
	if skip < 0 {
		skip = 0
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(skip))

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []models.AdminAuditLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *adminAuditRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}
