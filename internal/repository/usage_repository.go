// 2. Create the usage repository
// internal/repository/usage_repository.go
package repository

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type UsageRepository interface {
	CreateUsage(ctx context.Context, usage *models.ServiceUsage) error
	GetGlobalStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error)
	GetUserStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UserUsageStats, error)
	GetServiceUserStats(ctx context.Context, serviceName string, startDate, endDate *time.Time) ([]models.ServiceUserStats, error)
	GetUserUsageHistory(ctx context.Context, userID string, limit, skip int) ([]models.ServiceUsage, error)
	GetServiceUsageHistory(ctx context.Context, serviceName string, limit, skip int) ([]models.ServiceUsage, error)
}

type usageRepository struct {
	collection *mongo.Collection
}

func NewUsageRepository(collection *mongo.Collection) UsageRepository {
	return &usageRepository{
		collection: collection,
	}
}

func (r *usageRepository) CreateUsage(ctx context.Context, usage *models.ServiceUsage) error {
	usage.ID = primitive.NewObjectID()
	usage.CreatedAt = time.Now()
	
	_, err := r.collection.InsertOne(ctx, usage)
	return err
}

func (r *usageRepository) GetGlobalStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error) {
	pipeline := []bson.M{
		{
			"$match": r.buildDateFilter(startDate, endDate),
		},
		{
			"$group": bson.M{
				"_id": "$service_name",
				"total_calls": bson.M{"$sum": 1},
				"success_calls": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": "$success",
							"then": 1,
							"else": 0,
						},
					},
				},
				"failed_calls": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": "$success",
							"then": 0,
							"else": 1,
						},
					},
				},
				"total_credits": bson.M{"$sum": "$credits_used"},
			},
		},
		{
			"$sort": bson.M{"total_calls": -1},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []models.UsageStats
	if err = cursor.All(ctx, &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

func (r *usageRepository) GetUserStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UserUsageStats, error) {
	pipeline := []bson.M{
		{
			"$match": r.buildDateFilter(startDate, endDate),
		},
		{
			"$group": bson.M{
				"_id": "$user_id",
				"email": bson.M{"$first": "$email"},
				"total_calls": bson.M{"$sum": 1},
				"success_calls": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": "$success",
							"then": 1,
							"else": 0,
						},
					},
				},
				"failed_calls": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": "$success",
							"then": 0,
							"else": 1,
						},
					},
				},
				"total_credits": bson.M{"$sum": "$credits_used"},
			},
		},
		{
			"$sort": bson.M{"total_calls": -1},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []models.UserUsageStats
	if err = cursor.All(ctx, &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

func (r *usageRepository) GetServiceUserStats(ctx context.Context, serviceName string, startDate, endDate *time.Time) ([]models.ServiceUserStats, error) {
	matchFilter := r.buildDateFilter(startDate, endDate)
	if serviceName != "" {
		matchFilter["service_name"] = serviceName
	}

	pipeline := []bson.M{
		{
			"$match": matchFilter,
		},
		{
			"$group": bson.M{
				"_id": bson.M{
					"user_id": "$user_id",
					"service_name": "$service_name",
				},
				"email": bson.M{"$first": "$email"},
				"total_calls": bson.M{"$sum": 1},
				"success_calls": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": "$success",
							"then": 1,
							"else": 0,
						},
					},
				},
				"failed_calls": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": "$success",
							"then": 0,
							"else": 1,
						},
					},
				},
				"total_credits": bson.M{"$sum": "$credits_used"},
				"last_used": bson.M{"$max": "$created_at"},
			},
		},
		{
			"$project": bson.M{
				"user_id": "$_id.user_id",
				"email": 1,
				"service_name": "$_id.service_name",
				"total_calls": 1,
				"success_calls": 1,
				"failed_calls": 1,
				"total_credits": 1,
				"last_used": 1,
				"_id": 0,
			},
		},
		{
			"$sort": bson.M{"total_calls": -1},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stats []models.ServiceUserStats
	if err = cursor.All(ctx, &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

func (r *usageRepository) GetUserUsageHistory(ctx context.Context, userID string, limit, skip int) ([]models.ServiceUsage, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(skip))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var usage []models.ServiceUsage
	if err = cursor.All(ctx, &usage); err != nil {
		return nil, err
	}

	return usage, nil
}

func (r *usageRepository) GetServiceUsageHistory(ctx context.Context, serviceName string, limit, skip int) ([]models.ServiceUsage, error) {
	filter := bson.M{"service_name": serviceName}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(skip))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var usage []models.ServiceUsage
	if err = cursor.All(ctx, &usage); err != nil {
		return nil, err
	}

	return usage, nil
}

func (r *usageRepository) buildDateFilter(startDate, endDate *time.Time) bson.M {
	filter := bson.M{}
	
	if startDate != nil || endDate != nil {
		dateFilter := bson.M{}
		if startDate != nil {
			dateFilter["$gte"] = *startDate
		}
		if endDate != nil {
			dateFilter["$lte"] = *endDate
		}
		filter["created_at"] = dateFilter
	}
	
	return filter
}