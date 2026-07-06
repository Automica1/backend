// internal/repository/user_repository.go
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
)

// Remove the UserRepository interface from here - it's defined in interfaces.go

type userRepository struct {
	collection *mongo.Collection
}

func NewUserRepository(collection *mongo.Collection) UserRepository {
	return &userRepository{
		collection: collection,
	}
}

func (r *userRepository) Create(ctx context.Context, user *models.User) error {
	if !user.IsActive {
		user.IsActive = true
	}
	result, err := r.collection.InsertOne(ctx, user)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperrors.NewUserAlreadyExistsError()
		}
		return err
	}

	user.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *userRepository) GetByUserID(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	err := r.collection.FindOne(ctx, bson.M{"userId": userID}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewUserNotFoundError()
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := r.collection.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewUserNotFoundError()
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) IsSuspendedByEmail(ctx context.Context, email string) (bool, error) {
	var doc bson.M
	err := r.collection.FindOne(ctx, bson.M{"email": email}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, apperrors.NewUserNotFoundError()
		}
		return false, err
	}

	rawActive, ok := doc["isActive"]
	if !ok {
		return false, nil
	}

	isActive, ok := rawActive.(bool)
	if !ok {
		return false, nil
	}

	return !isActive, nil
}

func (r *userRepository) Delete(ctx context.Context, userID string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"userId": userID})
	return err
}

func (r *userRepository) UpdateActiveStatus(ctx context.Context, userID string, isActive bool) error {
	update := bson.M{
		"$set": bson.M{
			"isActive":  isActive,
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"userId": userID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewUserNotFoundError()
	}
	return nil
}

func (r *userRepository) UpdateBillingCurrency(ctx context.Context, userID string, currency string) error {
	update := bson.M{
		"$set": bson.M{
			"billingCurrency": currency,
			"updatedAt":       time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"userId": userID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewUserNotFoundError()
	}
	return nil
}

func (r *userRepository) ClearBillingCurrency(ctx context.Context, userID string) error {
	update := bson.M{
		"$unset": bson.M{"billingCurrency": ""},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"userId": userID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewUserNotFoundError()
	}
	return nil
}

func (r *userRepository) UpdateBetaFeedbackRefundCapOverride(ctx context.Context, userID string, cap *int) error {
	var update bson.M
	if cap == nil {
		update = bson.M{
			"$unset": bson.M{"betaFeedbackMonthlyRefundCapOverride": ""},
			"$set": bson.M{
				"updatedAt": time.Now(),
			},
		}
	} else {
		update = bson.M{
			"$set": bson.M{
				"betaFeedbackMonthlyRefundCapOverride": *cap,
				"updatedAt":                            time.Now(),
			},
		}
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"userId": userID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewUserNotFoundError()
	}
	return nil
}

func (r *userRepository) SetBetaFeedbackRefundBudgetResetAt(ctx context.Context, userID string, at time.Time) error {
	update := bson.M{
		"$set": bson.M{
			"betaFeedbackRefundBudgetResetAt": at,
			"updatedAt":                       time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"userId": userID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return apperrors.NewUserNotFoundError()
	}
	return nil
}

// Admin methods
func (r *userRepository) GetAll(ctx context.Context) ([]models.User, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var users []models.User
	if err = cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepository) GetTotalCount(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}
