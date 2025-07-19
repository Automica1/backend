// internal/repository/token_repository.go
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

type TokenRepository interface {
	Create(ctx context.Context, token *models.CreditToken) error
	GetByToken(ctx context.Context, token string) (*models.CreditToken, error)
	GetByID(ctx context.Context, id primitive.ObjectID) (*models.CreditToken, error)  // Add this line
	MarkAsUsed(ctx context.Context, token string, userID string) error
	GetByCreatedBy(ctx context.Context, createdBy string) ([]*models.CreditToken, error)
	GetAll(ctx context.Context) ([]*models.CreditToken, error)
	GetByStatus(ctx context.Context, isUsed bool) ([]*models.CreditToken, error)
	Delete(ctx context.Context, id primitive.ObjectID) error                           // Add this line
	DeleteExpiredTokens(ctx context.Context) error
}

type tokenRepository struct {
	collection *mongo.Collection
}

func NewTokenRepository(collection *mongo.Collection) TokenRepository {
	return &tokenRepository{
		collection: collection,
	}
}

func (r *tokenRepository) Create(ctx context.Context, token *models.CreditToken) error {
	result, err := r.collection.InsertOne(ctx, token)
	if err != nil {
		return err
	}
	
	token.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *tokenRepository) GetByToken(ctx context.Context, token string) (*models.CreditToken, error) {
	var creditToken models.CreditToken
	err := r.collection.FindOne(ctx, bson.M{"token": token}).Decode(&creditToken)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "token not found")
		}
		return nil, err
	}
	return &creditToken, nil
}

func (r *tokenRepository) MarkAsUsed(ctx context.Context, token string, userID string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"isUsed": true,
			"usedBy": userID,
			"usedAt": now,
		},
	}
	
	result, err := r.collection.UpdateOne(ctx, bson.M{"token": token}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "token not found")
	}
	return nil
}

func (r *tokenRepository) GetByCreatedBy(ctx context.Context, createdBy string) ([]*models.CreditToken, error) {
	// Sort by creation date (newest first)
	opts := options.Find().SetSort(bson.M{"createdAt": -1})
	
	cursor, err := r.collection.Find(ctx, bson.M{"createdBy": createdBy}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tokens []*models.CreditToken
	for cursor.Next(ctx) {
		var token models.CreditToken
		if err := cursor.Decode(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, &token)
	}

	return tokens, cursor.Err()
}

func (r *tokenRepository) GetAll(ctx context.Context) ([]*models.CreditToken, error) {
	// Sort by creation date (newest first)
	opts := options.Find().SetSort(bson.M{"createdAt": -1})
	
	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tokens []*models.CreditToken
	for cursor.Next(ctx) {
		var token models.CreditToken
		if err := cursor.Decode(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, &token)
	}

	return tokens, cursor.Err()
}

func (r *tokenRepository) GetByStatus(ctx context.Context, isUsed bool) ([]*models.CreditToken, error) {
	filter := bson.M{"isUsed": isUsed}
	
	// Sort by creation date (newest first)
	opts := options.Find().SetSort(bson.M{"createdAt": -1})
	
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tokens []*models.CreditToken
	for cursor.Next(ctx) {
		var token models.CreditToken
		if err := cursor.Decode(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, &token)
	}

	return tokens, cursor.Err()
}

func (r *tokenRepository) DeleteExpiredTokens(ctx context.Context) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{
		"expiresAt": bson.M{"$lt": time.Now()},
		"isUsed":    false,
	})
	return err
}

func (r *tokenRepository) GetByID(ctx context.Context, id primitive.ObjectID) (*models.CreditToken, error) {
	var creditToken models.CreditToken
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&creditToken)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "token not found")
		}
		return nil, err
	}
	return &creditToken, nil
}

func (r *tokenRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to delete token")
	}
	if result.DeletedCount == 0 {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "token not found")
	}
	return nil
}