// internal/repository/subscription_repository.go
package repository

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type SubscriptionRepository interface {
	Create(ctx context.Context, sub *models.Subscription) error
	GetByUserID(ctx context.Context, userID string) (*models.Subscription, error)
	GetBySubscriptionID(ctx context.Context, subID string) (*models.Subscription, error)
	Update(ctx context.Context, sub *models.Subscription) error
	UpdateStatus(ctx context.Context, subID string, status models.SubscriptionStatus) error
	GetAll(ctx context.Context) ([]models.Subscription, error)
	CountActive(ctx context.Context) (int64, error)
}

type subscriptionRepository struct {
	collection *mongo.Collection
}

func NewSubscriptionRepository(collection *mongo.Collection) SubscriptionRepository {
	return &subscriptionRepository{
		collection: collection,
	}
}

func (r *subscriptionRepository) Create(ctx context.Context, sub *models.Subscription) error {
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	sub.UpdatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, sub)
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create subscription", err.Error())
	}
	return nil
}

func (r *subscriptionRepository) GetByUserID(ctx context.Context, userID string) (*models.Subscription, error) {
	var sub models.Subscription
	// Sort by updatedAt descending to get the most recently updated (active) subscription first
	opts := options.FindOne().SetSort(bson.D{{Key: "updatedAt", Value: -1}})
	err := r.collection.FindOne(ctx, bson.M{"userId": userID}, opts).Decode(&sub)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "subscription not found for user", "")
		}
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get subscription", err.Error())
	}
	return &sub, nil
}

func (r *subscriptionRepository) GetBySubscriptionID(ctx context.Context, subID string) (*models.Subscription, error) {
	var sub models.Subscription
	err := r.collection.FindOne(ctx, bson.M{"subscriptionId": subID}).Decode(&sub)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "subscription not found", "")
		}
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get subscription", err.Error())
	}
	return &sub, nil
}

func (r *subscriptionRepository) Update(ctx context.Context, sub *models.Subscription) error {
	sub.UpdatedAt = time.Now()
	filter := bson.M{"subscriptionId": sub.SubscriptionID}
	update := bson.M{"$set": sub}

	_, err := r.collection.UpdateOne(ctx, filter, update, options.Update().SetUpsert(false))
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to update subscription", err.Error())
	}
	return nil
}

func (r *subscriptionRepository) UpdateStatus(ctx context.Context, subID string, status models.SubscriptionStatus) error {
	filter := bson.M{"subscriptionId": subID}
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to update subscription status", err.Error())
	}
	return nil
}

func (r *subscriptionRepository) GetAll(ctx context.Context) ([]models.Subscription, error) {
	var subs []models.Subscription
	cursor, err := r.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get subscriptions", err.Error())
	}
	defer cursor.Close(ctx)

	if err := cursor.All(ctx, &subs); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to decode subscriptions", err.Error())
	}

	return subs, nil
}

func (r *subscriptionRepository) CountActive(ctx context.Context) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"status": models.SubscriptionStatusActive})
	if err != nil {
		return 0, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to count active subscriptions", err.Error())
	}
	return count, nil
}
