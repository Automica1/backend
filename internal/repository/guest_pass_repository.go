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

type GuestPassRepository interface {
	Create(ctx context.Context, pass *models.GuestPass) error
	GetActiveByHash(ctx context.Context, keyHash string) (*models.GuestPass, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*models.GuestPass, error)
	List(ctx context.Context) ([]*models.GuestPass, error)
	Update(ctx context.Context, id primitive.ObjectID, update bson.M) error
	Revoke(ctx context.Context, id primitive.ObjectID) error
	UpdateLastUsed(ctx context.Context, keyHash string) error
	SyncRemainingCredits(ctx context.Context, id primitive.ObjectID, remaining int) error
}

type guestPassRepository struct {
	collection *mongo.Collection
}

func NewGuestPassRepository(collection *mongo.Collection) GuestPassRepository {
	return &guestPassRepository{collection: collection}
}

func (r *guestPassRepository) Create(ctx context.Context, pass *models.GuestPass) error {
	result, err := r.collection.InsertOne(ctx, pass)
	if err != nil {
		return err
	}
	pass.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *guestPassRepository) GetActiveByHash(ctx context.Context, keyHash string) (*models.GuestPass, error) {
	filter := bson.M{
		"keyHash":  keyHash,
		"isActive": true,
		"$or": []bson.M{
			{"revokedAt": bson.M{"$exists": false}},
			{"revokedAt": nil},
		},
		"$and": []bson.M{
			{
				"$or": []bson.M{
					{"expiresAt": bson.M{"$exists": false}},
					{"expiresAt": nil},
					{"expiresAt": bson.M{"$gt": time.Now()}},
				},
			},
		},
	}

	var pass models.GuestPass
	err := r.collection.FindOne(ctx, filter).Decode(&pass)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "invalid or expired guest pass")
		}
		return nil, err
	}
	return &pass, nil
}

func (r *guestPassRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.GuestPass, error) {
	var pass models.GuestPass
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&pass)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "guest pass not found")
		}
		return nil, err
	}
	return &pass, nil
}

func (r *guestPassRepository) List(ctx context.Context) ([]*models.GuestPass, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var passes []*models.GuestPass
	if err := cursor.All(ctx, &passes); err != nil {
		return nil, err
	}
	return passes, nil
}

func (r *guestPassRepository) Update(ctx context.Context, id primitive.ObjectID, update bson.M) error {
	update["updatedAt"] = time.Now()
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "guest pass not found")
	}
	return nil
}

func (r *guestPassRepository) Revoke(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	result, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id, "isActive": true},
		bson.M{"$set": bson.M{
			"isActive":  false,
			"revokedAt": now,
			"updatedAt": now,
		}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "guest pass not found or already revoked")
	}
	return nil
}

func (r *guestPassRepository) UpdateLastUsed(ctx context.Context, keyHash string) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"keyHash": keyHash},
		bson.M{
			"$set": bson.M{"lastUsedAt": now, "updatedAt": now},
			"$inc": bson.M{"usageCount": 1},
		},
	)
	return err
}

func (r *guestPassRepository) SyncRemainingCredits(ctx context.Context, id primitive.ObjectID, remaining int) error {
	return r.Update(ctx, id, bson.M{"remainingCredits": remaining})
}
