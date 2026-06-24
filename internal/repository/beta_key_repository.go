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

type BetaKeyRepository interface {
	Create(ctx context.Context, betaKey *models.BetaKey) error
	GetActiveByHash(ctx context.Context, keyHash string) (*models.BetaKey, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaKey, error)
	List(ctx context.Context, serviceName string) ([]*models.BetaKey, error)
	Revoke(ctx context.Context, id primitive.ObjectID) error
	UpdateLastUsed(ctx context.Context, keyHash string) error
}

type betaKeyRepository struct {
	collection *mongo.Collection
}

func NewBetaKeyRepository(collection *mongo.Collection) BetaKeyRepository {
	return &betaKeyRepository{collection: collection}
}

func (r *betaKeyRepository) Create(ctx context.Context, betaKey *models.BetaKey) error {
	result, err := r.collection.InsertOne(ctx, betaKey)
	if err != nil {
		return err
	}
	betaKey.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *betaKeyRepository) GetActiveByHash(ctx context.Context, keyHash string) (*models.BetaKey, error) {
	filter := bson.M{
		"keyHash":   keyHash,
		"isActive":  true,
		"revokedAt": bson.M{"$exists": false},
		"$or": []bson.M{
			{"expiresAt": bson.M{"$exists": false}},
			{"expiresAt": nil},
			{"expiresAt": bson.M{"$gt": time.Now()}},
		},
	}

	var betaKey models.BetaKey
	err := r.collection.FindOne(ctx, filter).Decode(&betaKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "invalid or expired beta key")
		}
		return nil, err
	}
	return &betaKey, nil
}

func (r *betaKeyRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaKey, error) {
	var betaKey models.BetaKey
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&betaKey)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta key not found")
		}
		return nil, err
	}
	return &betaKey, nil
}

func (r *betaKeyRepository) List(ctx context.Context, serviceName string) ([]*models.BetaKey, error) {
	filter := bson.M{}
	if serviceName != "" {
		filter["serviceName"] = serviceName
	}

	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var keys []*models.BetaKey
	if err := cursor.All(ctx, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (r *betaKeyRepository) Revoke(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id, "isActive": true},
		bson.M{"$set": bson.M{
			"isActive":  false,
			"revokedAt": now,
		}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta key not found or already revoked")
	}
	return nil
}

func (r *betaKeyRepository) UpdateLastUsed(ctx context.Context, keyHash string) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"keyHash": keyHash},
		bson.M{
			"$set": bson.M{"lastUsedAt": now},
			"$inc": bson.M{"usageCount": 1},
		},
	)
	return err
}
