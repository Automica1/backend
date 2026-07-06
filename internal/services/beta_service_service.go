package services

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
)

type BetaServiceService interface {
	Create(ctx context.Context, req *models.CreateBetaServiceRequest) (*models.BetaService, error)
	GetByTag(ctx context.Context, tag string) (*models.BetaService, error)
	GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error)
	List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error)
	Update(ctx context.Context, tag string, req *models.UpdateBetaServiceRequest) (*models.BetaService, error)
}

type betaServiceService struct {
	repo repository.BetaServiceRepository
}

func NewBetaServiceService(repo repository.BetaServiceRepository) BetaServiceService {
	return &betaServiceService{repo: repo}
}

func (s *betaServiceService) Create(ctx context.Context, req *models.CreateBetaServiceRequest) (*models.BetaService, error) {
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	now := time.Now()
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	service := &models.BetaService{
		Tag:         req.Tag,
		ServiceName: req.ServiceName,
		Label:       req.Label,
		APIURL:      req.APIURL,
		IsActive:    isActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.Create(ctx, service); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *betaServiceService) GetByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return s.repo.GetByTag(ctx, tag)
}

func (s *betaServiceService) GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return s.repo.GetActiveByTag(ctx, tag)
}

func (s *betaServiceService) List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error) {
	return s.repo.List(ctx, serviceName, activeOnly)
}

func (s *betaServiceService) Update(ctx context.Context, tag string, req *models.UpdateBetaServiceRequest) (*models.BetaService, error) {
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	update := bson.M{}
	if req.Label != nil {
		update["label"] = *req.Label
	}
	if req.APIURL != nil {
		update["apiUrl"] = *req.APIURL
	}
	if req.IsActive != nil {
		update["isActive"] = *req.IsActive
	}

	return s.repo.Update(ctx, tag, update)
}
