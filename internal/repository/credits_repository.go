// internal/repository/credits_repository.go
package repository

import (
	"context"
	"errors"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type creditsRepository struct {
	collection *mongo.Collection
}

func NewCreditsRepository(collection *mongo.Collection) CreditsRepository {
	return &creditsRepository{
		collection: collection,
	}
}

func (r *creditsRepository) Create(ctx context.Context, credits *models.Credits) error {
	result, err := r.collection.InsertOne(ctx, credits)
	if err != nil {
		return err
	}
	
	credits.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *creditsRepository) GetByUserID(ctx context.Context, userID string) (*models.Credits, error) {
	var credits models.Credits
	err := r.collection.FindOne(ctx, bson.M{"userId": userID}).Decode(&credits)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewCreditsNotFoundError()
		}
		return nil, err
	}
	return &credits, nil
}

func (r *creditsRepository) UpdateCredits(ctx context.Context, userID string, amount int) error {
	update := bson.M{"$inc": bson.M{"credits": amount}}
	result, err := r.collection.UpdateOne(ctx, bson.M{"userId": userID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewCreditsNotFoundError()
	}
	return nil
}

func (r *creditsRepository) DeductCredits(ctx context.Context, userID string, amount int) error {
	// First check if user has enough credits
	credits, err := r.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	
	if credits.Credits < amount {
		return apperrors.NewInsufficientCreditsError()
	}
	
	return r.UpdateCredits(ctx, userID, -amount)
}

func (r *creditsRepository) GetTotalCredits(ctx context.Context) (int64, error) {
	pipeline := []bson.M{
		{
			"$group": bson.M{
				"_id":   nil,
				"total": bson.M{"$sum": "$credits"},
			},
		},
	}
	
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	
	var result struct {
		Total int64 `bson:"total"`
	}
	
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0, err
		}
		return result.Total, nil
	}
	
	return 0, nil // No credits found
}

func (r *creditsRepository) GetAllWithUsers(ctx context.Context) ([]models.AdminUser, error) {
	pipeline := []bson.M{
		{
			"$lookup": bson.M{
				"from":         "users", // Assuming your users collection is named "users"
				"localField":   "userId",
				"foreignField": "userId", 
				"as":           "userInfo",
			},
		},
		{
			"$unwind": "$userInfo",
		},
		{
			"$project": bson.M{
				"_id":       "$userInfo._id",
				"userId":    "$userId",
				"email":     "$userInfo.email",
				"credits":   "$credits",
				"createdAt": "$userInfo.createdAt",
				"updatedAt": "$userInfo.updatedAt",
			},
		},
	}
	
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	
	var adminUsers []models.AdminUser
	if err = cursor.All(ctx, &adminUsers); err != nil {
		return nil, err
	}
	
	return adminUsers, nil
}