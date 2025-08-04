// internal/repository/user_repository.go
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

func (r *userRepository) Delete(ctx context.Context, userID string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"userId": userID})
	return err
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