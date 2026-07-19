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

type BetaServiceRepository interface {
	Create(ctx context.Context, service *models.BetaService) error
	GetByTag(ctx context.Context, tag string) (*models.BetaService, error)
	GetActiveByServiceName(ctx context.Context, serviceName string) (*models.BetaService, error)
	GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error)
	List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error)
	Update(ctx context.Context, tag string, update bson.M) (*models.BetaService, error)
}

type betaServiceRepository struct {
	collection *mongo.Collection
}

func NewBetaServiceRepository(collection *mongo.Collection) BetaServiceRepository {
	return &betaServiceRepository{collection: collection}
}

func (r *betaServiceRepository) Create(ctx context.Context, service *models.BetaService) error {
	result, err := r.collection.InsertOne(ctx, service)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return apperrors.NewAppError(apperrors.ErrValidation, 400, "beta service tag already exists")
		}
		return err
	}
	service.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *betaServiceRepository) GetByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	var service models.BetaService
	err := r.collection.FindOne(ctx, bson.M{"tag": tag}).Decode(&service)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta service not found")
		}
		return nil, err
	}
	return &service, nil
}

func (r *betaServiceRepository) GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	var service models.BetaService
	err := r.collection.FindOne(ctx, bson.M{
		"tag":      tag,
		"isActive": true,
	}).Decode(&service)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "beta service is not available")
		}
		return nil, err
	}
	return &service, nil
}

func (r *betaServiceRepository) GetActiveByServiceName(ctx context.Context, serviceName string) (*models.BetaService, error) {
	var service models.BetaService
	err := r.collection.FindOne(
		ctx,
		bson.M{
			"serviceName": serviceName,
			"isActive":    true,
		},
		options.FindOne().SetSort(bson.D{{Key: "updatedAt", Value: -1}}),
	).Decode(&service)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta service not found")
		}
		return nil, err
	}
	return &service, nil
}

func (r *betaServiceRepository) List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error) {
	filter := bson.M{}
	if serviceName != "" {
		filter["serviceName"] = serviceName
	}
	if activeOnly {
		filter["isActive"] = true
	}

	opts := options.Find().SetSort(bson.D{{Key: "label", Value: 1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var services []*models.BetaService
	if err := cursor.All(ctx, &services); err != nil {
		return nil, err
	}
	return services, nil
}

func (r *betaServiceRepository) Update(ctx context.Context, tag string, update bson.M) (*models.BetaService, error) {
	update["updatedAt"] = time.Now()
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After)

	var service models.BetaService
	err := r.collection.FindOneAndUpdate(
		ctx,
		bson.M{"tag": tag},
		bson.M{"$set": update},
		opts,
	).Decode(&service)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "beta service not found")
		}
		return nil, err
	}
	return &service, nil
}
