// internal/repository/plan_repository.go
package repository

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type PlanRepository interface {
	Create(ctx context.Context, plan *models.Plan) error
	GetAll(ctx context.Context, onlyActive bool) ([]models.Plan, error)
	GetByPlanID(ctx context.Context, planID string) (*models.Plan, error)
	Update(ctx context.Context, planID string, updates bson.M) error
	Delete(ctx context.Context, planID string) error
}

type planRepository struct {
	collection *mongo.Collection
}

func NewPlanRepository(collection *mongo.Collection) PlanRepository {
	return &planRepository{
		collection: collection,
	}
}

func (r *planRepository) Create(ctx context.Context, plan *models.Plan) error {
	plan.CreatedAt = time.Now()
	plan.UpdatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, plan)
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create plan", err.Error())
	}
	return nil
}

func (r *planRepository) GetAll(ctx context.Context, onlyActive bool) ([]models.Plan, error) {
	filter := bson.M{}
	if onlyActive {
		filter["isActive"] = true
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get plans", err.Error())
	}
	defer cursor.Close(ctx)

	var plans []models.Plan
	if err := cursor.All(ctx, &plans); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to decode plans", err.Error())
	}

	if plans == nil {
		plans = []models.Plan{}
	}

	return plans, nil
}

func (r *planRepository) GetByPlanID(ctx context.Context, planID string) (*models.Plan, error) {
	var plan models.Plan
	err := r.collection.FindOne(ctx, bson.M{"planId": planID}).Decode(&plan)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "plan not found", "")
		}
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to get plan", err.Error())
	}
	return &plan, nil
}

func (r *planRepository) Update(ctx context.Context, planID string, updates bson.M) error {
	updates["updatedAt"] = time.Now()
	filter := bson.M{"planId": planID}
	update := bson.M{"$set": updates}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to update plan", err.Error())
	}
	return nil
}

func (r *planRepository) Delete(ctx context.Context, planID string) error {
	// We do a soft delete by setting isActive to false
	filter := bson.M{"planId": planID}
	update := bson.M{"$set": bson.M{"isActive": false, "updatedAt": time.Now()}}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to deactivate plan", err.Error())
	}
	return nil
}
