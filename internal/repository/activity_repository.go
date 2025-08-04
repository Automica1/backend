// internal/repository/activity_repository.go
package repository

import (
	"context"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ActivityRepository interface is defined in interfaces.go

type activityRepository struct {
	collection *mongo.Collection
}

func NewActivityRepository(collection *mongo.Collection) ActivityRepository {
	return &activityRepository{
		collection: collection,
	}
}

func (r *activityRepository) Create(ctx context.Context, activity *models.ActivityLog) error {
	_, err := r.collection.InsertOne(ctx, activity)
	return err
}

func (r *activityRepository) GetByUserID(ctx context.Context, userID string) ([]models.ActivityLog, error) {
	// Sort by timestamp descending (most recent first)
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}})
	
	cursor, err := r.collection.Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	
	var activities []models.ActivityLog
	if err = cursor.All(ctx, &activities); err != nil {
		return nil, err
	}
	
	return activities, nil
}