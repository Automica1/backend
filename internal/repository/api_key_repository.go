// internal/repository/api_key_repository.go
package repository

import (
	"context"
	"errors"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type APIKeyRepository interface {
	Create(ctx context.Context, apiKey *models.APIKey) error
	GetByHash(ctx context.Context, keyHash string) (*models.APIKey, error)
	GetByUserID(ctx context.Context, userID string) (*models.APIKey, error) // Changed return type from slice to single key
	GetByID(ctx context.Context, id primitive.ObjectID) (*models.APIKey, error)
	Update(ctx context.Context, id primitive.ObjectID, update bson.M) error
	Delete(ctx context.Context, id primitive.ObjectID, userID string) error
	DeleteByUserID(ctx context.Context, userID string) error // New method to delete by userID
	UpdateLastUsed(ctx context.Context, keyHash string) error
	GetActiveByHash(ctx context.Context, keyHash string) (*models.APIKey, error)
}

type apiKeyRepository struct {
	collection *mongo.Collection
}

func NewAPIKeyRepository(collection *mongo.Collection) APIKeyRepository {
	return &apiKeyRepository{
		collection: collection,
	}
}

func (r *apiKeyRepository) Create(ctx context.Context, apiKey *models.APIKey) error {
	result, err := r.collection.InsertOne(ctx, apiKey)
	if err != nil {
		return err
	}
	
	apiKey.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *apiKeyRepository) GetByHash(ctx context.Context, keyHash string) (*models.APIKey, error) {
	var apiKey models.APIKey
	err := r.collection.FindOne(ctx, bson.M{"keyHash": keyHash}).Decode(&apiKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(
				apperrors.ErrNotFound,
				404,
				"API key not found",
			)
		}
		return nil, err
	}
	return &apiKey, nil
}

func (r *apiKeyRepository) GetActiveByHash(ctx context.Context, keyHash string) (*models.APIKey, error) {
	filter := bson.M{
		"keyHash":  keyHash,
		"isActive": true,
		"$or": []bson.M{
			{"expiresAt": bson.M{"$exists": false}},
			{"expiresAt": nil},
			{"expiresAt": bson.M{"$gt": time.Now()}},
		},
	}
	
	var apiKey models.APIKey
	err := r.collection.FindOne(ctx, filter).Decode(&apiKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(
				apperrors.ErrNotFound,
				404,
				"active API key not found",
			)
		}
		return nil, err
	}
	return &apiKey, nil
}

// GetByUserID now returns a single API key instead of a slice
func (r *apiKeyRepository) GetByUserID(ctx context.Context, userID string) (*models.APIKey, error) {
	filter := bson.M{"userId": userID}
	opts := options.FindOne().SetSort(bson.M{"createdAt": -1}) // Get the most recent one
	
	var apiKey models.APIKey
	err := r.collection.FindOne(ctx, filter, opts).Decode(&apiKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(
				apperrors.ErrNotFound,
				404,
				"API key not found",
			)
		}
		return nil, err
	}
	
	return &apiKey, nil
}

func (r *apiKeyRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.APIKey, error) {
	var apiKey models.APIKey
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&apiKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(
				apperrors.ErrNotFound,
				404,
				"API key not found",
			)
		}
		return nil, err
	}
	return &apiKey, nil
}

func (r *apiKeyRepository) Update(ctx context.Context, id primitive.ObjectID, update bson.M) error {
	update["updatedAt"] = time.Now()
	
	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": update},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewAppError(
			apperrors.ErrNotFound,
			404,
			"API key not found",
		)
	}
	return nil
}

func (r *apiKeyRepository) Delete(ctx context.Context, id primitive.ObjectID, userID string) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{
		"_id":    id,
		"userId": userID, // Ensure user can only delete their own keys
	})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return apperrors.NewAppError(
			apperrors.ErrNotFound,
			404,
			"API key not found or access denied",
		)
	}
	return nil
}

// DeleteByUserID deletes all API keys for a specific user
func (r *apiKeyRepository) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return err
	}
	// Note: We don't check DeletedCount here because it's okay if no keys exist
	return nil
}

func (r *apiKeyRepository) UpdateLastUsed(ctx context.Context, keyHash string) error {
	now := time.Now()
	update := bson.M{
		"lastUsedAt":  &now,
		"updatedAt":   now,
		"$inc": bson.M{"usageCount": 1},
	}
	
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"keyHash": keyHash},
		bson.M{"$set": update},
	)
	return err
}