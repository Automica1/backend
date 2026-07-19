// internal/services/credits_service.go
package services

import (
	"context"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
)

type CreditsService interface {
	GetBalance(ctx context.Context, userID string) (*models.CreditsResponse, error)
	GetBalanceByEmail(ctx context.Context, email string) (*models.CreditsResponse, error)
	AddCredits(ctx context.Context, req *models.AddCreditsRequest) (*models.CreditsResponse, error)
	DeductCredits(ctx context.Context, req *models.DeductCreditsRequest) (*models.CreditsResponse, error)
}

type creditsService struct {
	creditsRepo repository.CreditsRepository
	userRepo    repository.UserRepository
}

func NewCreditsService(creditsRepo repository.CreditsRepository, userRepo repository.UserRepository) CreditsService {
	return &creditsService{
		creditsRepo: creditsRepo,
		userRepo:    userRepo,
	}
}

func (s *creditsService) GetBalance(ctx context.Context, userID string) (*models.CreditsResponse, error) {
	credits, err := s.creditsRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &models.CreditsResponse{
		Message: "Credits balance retrieved successfully",
		UserID:  userID,
		Credits: credits.Credits,
	}, nil
}

func (s *creditsService) GetBalanceByEmail(ctx context.Context, email string) (*models.CreditsResponse, error) {
	// First get user by email
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	// Then get credits balance
	return s.GetBalance(ctx, user.UserID)
}

func (s *creditsService) AddCredits(ctx context.Context, req *models.AddCreditsRequest) (*models.CreditsResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Atomically upsert credits: creates record if missing, increments if exists
	updatedCredits, err := s.creditsRepo.UpsertCredits(ctx, req.UserID, req.Amount)
	if err != nil {
		return nil, err
	}

	return &models.CreditsResponse{
		Message: "Credits added successfully",
		UserID:  req.UserID,
		Credits: updatedCredits.Credits,
	}, nil
}

// internal/services/credits_service.go
func (s *creditsService) DeductCredits(ctx context.Context, req *models.DeductCreditsRequest) (*models.CreditsResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Atomic conditional deduction; returns the balance after the update.
	updatedCredits, err := s.creditsRepo.DeductCredits(ctx, req.UserID, req.Amount)
	if err != nil {
		return nil, err
	}

	return &models.CreditsResponse{
		Message: "Credits deducted successfully",
		UserID:  req.UserID,
		Credits: updatedCredits.Credits,
	}, nil
}
