// internal/services/token_service.go
package services

import (
	"context"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
	
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type CreditTokenService interface {
	GenerateToken(ctx context.Context, req *models.GenerateTokenRequest, createdBy string) (*models.TokenResponse, error)
	RedeemToken(ctx context.Context, req *models.RedeemTokenRequest, userID string) (*models.TokenResponse, error)
	GetTokensByCreatedBy(ctx context.Context, createdBy string) ([]*models.CreditToken, error)
	GetAllTokens(ctx context.Context) ([]*models.CreditToken, error)
	GetTokensByStatus(ctx context.Context, isUsed bool) ([]*models.CreditToken, error)
	DeleteToken(ctx context.Context, tokenID string, adminEmail string) (*models.TokenResponse, error)
}

type creditTokenService struct {
	tokenRepo   repository.TokenRepository
	creditsRepo repository.CreditsRepository
}

func NewCreditTokenService(tokenRepo repository.TokenRepository, creditsRepo repository.CreditsRepository) CreditTokenService {
	return &creditTokenService{
		tokenRepo:   tokenRepo,
		creditsRepo: creditsRepo,
	}
}

func (s *creditTokenService) GenerateToken(ctx context.Context, req *models.GenerateTokenRequest, createdBy string) (*models.TokenResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Generate unique token
	tokenStr, err := models.GenerateToken()
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to generate token")
	}

	// Create token with 30-day expiry
	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)

	token := &models.CreditToken{
		Token:       tokenStr,
		Credits:     req.Credits,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		IsUsed:      false,
		Description: req.Description,
	}

	// Save to database
	if err := s.tokenRepo.Create(ctx, token); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create token")
	}

	return &models.TokenResponse{
		Message:     "Token generated successfully",
		Token:       tokenStr,
		Credits:     req.Credits,
		ExpiresAt:   expiresAt,
		Description: req.Description,
	}, nil
}

func (s *creditTokenService) RedeemToken(ctx context.Context, req *models.RedeemTokenRequest, userID string) (*models.TokenResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Get token from database
	token, err := s.tokenRepo.GetByToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}

	// Check if token is already used
	if token.IsUsed {
		return nil, apperrors.NewAppError(apperrors.ErrBadRequest, 400, "token has already been used")
	}

	// Check if token is expired
	if token.IsExpired() {
		return nil, apperrors.NewAppError(apperrors.ErrBadRequest, 400, "token has expired")
	}

	// Add credits to user
	if err := s.creditsRepo.UpdateCredits(ctx, userID, token.Credits); err != nil {
		return nil, err
	}

	// Mark token as used
	if err := s.tokenRepo.MarkAsUsed(ctx, req.Token, userID); err != nil {
		return nil, err
	}

	// Get updated credits balance after redeeming
	userCredits, err := s.creditsRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	return &models.TokenResponse{
		Message:          "Token redeemed successfully",
		Credits:          token.Credits,
		RemainingCredits: userCredits.Credits, // Include remaining credits
		UsedAt:           &now,
		Description:      token.Description,
	}, nil
}

func (s *creditTokenService) GetTokensByCreatedBy(ctx context.Context, createdBy string) ([]*models.CreditToken, error) {
	return s.tokenRepo.GetByCreatedBy(ctx, createdBy)
}

func (s *creditTokenService) GetAllTokens(ctx context.Context) ([]*models.CreditToken, error) {
	return s.tokenRepo.GetAll(ctx)
}

func (s *creditTokenService) GetTokensByStatus(ctx context.Context, isUsed bool) ([]*models.CreditToken, error) {
	return s.tokenRepo.GetByStatus(ctx, isUsed)
}

func (s *creditTokenService) DeleteToken(ctx context.Context, tokenID string, adminEmail string) (*models.TokenResponse, error) {
	// Validate tokenID format (ObjectID)
	objID, err := primitive.ObjectIDFromHex(tokenID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrBadRequest, 400, "invalid token ID format")
	}

	// Get the token first to check ownership and get details
	token, err := s.tokenRepo.GetByID(ctx, objID)
	if err != nil {
		return nil, err
	}

	// Check if the admin is the creator of the token (optional security check)
	if token.CreatedBy != adminEmail {
		return nil, apperrors.NewAppError(
			apperrors.ErrForbidden, 
			403, 
			"you can only delete tokens you created",
		)
	}

	// Check if token is already used (optional business rule)
	if token.IsUsed {
		return nil, apperrors.NewAppError(
			apperrors.ErrBadRequest, 
			400, 
			"cannot delete used tokens",
		)
	}

	// Delete the token
	if err := s.tokenRepo.Delete(ctx, objID); err != nil {
		return nil, err
	}

	return &models.TokenResponse{
		Message:     "Token deleted successfully",
		Credits:     token.Credits,
		Description: token.Description,
	}, nil
}