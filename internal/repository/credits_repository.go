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
	"go.mongodb.org/mongo-driver/mongo/options"
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

// UpsertCredits atomically creates a credits record if it doesn't exist, or increments
// the credits field if it does. Returns the new credit balance after the operation.
func (r *creditsRepository) UpsertCredits(ctx context.Context, userID string, amount int) (*models.Credits, error) {
	filter := bson.M{"userId": userID}
	update := bson.M{
		"$inc":         bson.M{"credits": amount},
		"$setOnInsert": bson.M{"userId": userID},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var result models.Credits
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeductCredits atomically deducts credits using a single conditional update:
// the document must match both the userID and credits >= amount, so concurrent
// deductions can never drive the balance negative. Returns the record after
// the deduction.
func (r *creditsRepository) DeductCredits(ctx context.Context, userID string, amount int) (*models.Credits, error) {
	filter := deductCreditsFilter(userID, amount)
	update := bson.M{"$inc": bson.M{"credits": -amount}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var result models.Credits
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&result)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Conditional update did not match: distinguish a missing record from
			// an insufficient balance to preserve the existing error contract.
			if _, getErr := r.GetByUserID(ctx, userID); getErr != nil {
				return nil, getErr
			}
			return nil, apperrors.NewInsufficientCreditsError()
		}
		return nil, err
	}
	return &result, nil
}

func deductCreditsFilter(userID string, amount int) bson.M {
	return bson.M{
		"userId":  userID,
		"credits": bson.M{"$gte": amount},
	}
}

func (r *creditsRepository) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"userId": userID})
	return err
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
				"isActive":  "$userInfo.isActive",
				"credits":   "$credits",
				"createdAt": "$userInfo.createdAt",
				"updatedAt": "$userInfo.updatedAt",
			},
		},
		{
			"$sort": bson.M{"createdAt": -1},
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
