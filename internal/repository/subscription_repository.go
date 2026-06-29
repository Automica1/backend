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
	GetByUserIDAndStatus(ctx context.Context, userID string, status models.SubscriptionStatus) (*models.Subscription, error)
	GetBySubscriptionID(ctx context.Context, subID string) (*models.Subscription, error)
	ListAllByUserID(ctx context.Context, userID string) ([]models.Subscription, error)
	DeleteByUserID(ctx context.Context, userID string) (int64, error)
	List(ctx context.Context, query models.AdminSubscriptionQuery) ([]models.Subscription, int64, error)
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
	// Only fetch primary subscription records (active, cancelled, past_due, etc.)
	// This avoids picking up temporary upgrade or one-time payment records
	filter := bson.M{
		"userId": userID,
		"status": bson.M{"$in": []models.SubscriptionStatus{
			models.SubscriptionStatusActive,
			models.SubscriptionStatusCancelled,
			models.SubscriptionStatusPastDue,
			models.SubscriptionStatusExpired,
			models.SubscriptionStatusCreated,
		}},
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "updatedAt", Value: -1}})
	err := r.collection.FindOne(ctx, filter, opts).Decode(&sub)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "subscription not found for user", "")
		}
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get subscription", err.Error())
	}
	return &sub, nil
}

func (r *subscriptionRepository) GetByUserIDAndStatus(ctx context.Context, userID string, status models.SubscriptionStatus) (*models.Subscription, error) {
	var sub models.Subscription
	filter := bson.M{
		"userId": userID,
		"status": status,
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "updatedAt", Value: -1}})
	err := r.collection.FindOne(ctx, filter, opts).Decode(&sub)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "no subscription with status "+string(status)+" found", "")
		}
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get subscription by status", err.Error())
	}
	return &sub, nil
}

func (r *subscriptionRepository) ListAllByUserID(ctx context.Context, userID string) ([]models.Subscription, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"userId": userID}, options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}))
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to list user subscriptions", err.Error())
	}
	defer cursor.Close(ctx)

	var subs []models.Subscription
	if err := cursor.All(ctx, &subs); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to decode user subscriptions", err.Error())
	}
	return subs, nil
}

func (r *subscriptionRepository) DeleteByUserID(ctx context.Context, userID string) (int64, error) {
	result, err := r.collection.DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return 0, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to delete user subscriptions", err.Error())
	}
	return result.DeletedCount, nil
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

func (r *subscriptionRepository) List(ctx context.Context, query models.AdminSubscriptionQuery) ([]models.Subscription, int64, error) {
	filter := bson.M{}

	if query.Status != "" {
		filter["status"] = query.Status
	}
	if query.UserID != "" {
		filter["userId"] = query.UserID
	}
	if query.Email != "" {
		filter["email"] = bson.M{"$regex": query.Email, "$options": "i"}
	}
	if query.PlanID != "" {
		filter["planId"] = query.PlanID
	}
	if query.SubscriptionID != "" {
		filter["subscriptionId"] = bson.M{"$regex": query.SubscriptionID, "$options": "i"}
	}
	if query.Search != "" {
		search := bson.M{
			"$or": []bson.M{
				{"userId": bson.M{"$regex": query.Search, "$options": "i"}},
				{"email": bson.M{"$regex": query.Search, "$options": "i"}},
				{"planId": bson.M{"$regex": query.Search, "$options": "i"}},
				{"subscriptionId": bson.M{"$regex": query.Search, "$options": "i"}},
				{"status": bson.M{"$regex": query.Search, "$options": "i"}},
			},
		}
		if len(filter) == 0 {
			filter = search
		} else {
			filter["$and"] = []bson.M{search}
		}
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to count subscriptions", err.Error())
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	skip := query.Skip
	if skip < 0 {
		skip = 0
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(skip))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get subscriptions", err.Error())
	}
	defer cursor.Close(ctx)

	var subs []models.Subscription
	if err := cursor.All(ctx, &subs); err != nil {
		return nil, 0, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to decode subscriptions", err.Error())
	}

	return subs, total, nil
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
