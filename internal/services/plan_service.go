// internal/services/plan_service.go
package services

import (
	"context"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"

	"go.mongodb.org/mongo-driver/bson"
)

type PlanService interface {
	CreatePlan(ctx context.Context, req *models.CreatePlanRequest) (*models.Plan, error)
	GetActivePlans(ctx context.Context) ([]models.Plan, error)
	GetAllPlans(ctx context.Context) ([]models.Plan, error)
	GetPlanByID(ctx context.Context, planID string) (*models.Plan, error)
	UpdatePlan(ctx context.Context, planID string, req *models.UpdatePlanRequest) error
	DeletePlan(ctx context.Context, planID string) error
}

type planService struct {
	planRepo repository.PlanRepository
}

func NewPlanService(planRepo repository.PlanRepository) PlanService {
	return &planService{
		planRepo: planRepo,
	}
}

func (s *planService) CreatePlan(ctx context.Context, req *models.CreatePlanRequest) (*models.Plan, error) {
	plan := &models.Plan{
		PlanID:         req.PlanID,
		RazorpayPlanID: req.RazorpayPlanID,
		Name:           req.Name,
		Description:    req.Description,
		Price:          req.Price,
		Credits:        req.Credits,
		IsActive:       req.IsActive,
	}

	err := s.planRepo.Create(ctx, plan)
	if err != nil {
		return nil, err
	}

	return plan, nil
}

func (s *planService) GetActivePlans(ctx context.Context) ([]models.Plan, error) {
	return s.planRepo.GetAll(ctx, true)
}

func (s *planService) GetAllPlans(ctx context.Context) ([]models.Plan, error) {
	return s.planRepo.GetAll(ctx, false)
}

func (s *planService) GetPlanByID(ctx context.Context, planID string) (*models.Plan, error) {
	return s.planRepo.GetByPlanID(ctx, planID)
}

func (s *planService) UpdatePlan(ctx context.Context, planID string, req *models.UpdatePlanRequest) error {
	updates := bson.M{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Price != nil {
		updates["price"] = *req.Price
	}
	if req.Credits != nil {
		updates["credits"] = *req.Credits
	}
	if req.IsActive != nil {
		updates["isActive"] = *req.IsActive
	}
	if req.RazorpayPlanID != nil {
		updates["razorpayPlanId"] = *req.RazorpayPlanID
	}

	if len(updates) == 0 {
		return nil
	}

	return s.planRepo.Update(ctx, planID, updates)
}

func (s *planService) DeletePlan(ctx context.Context, planID string) error {
	return s.planRepo.Delete(ctx, planID)
}
