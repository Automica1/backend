// internal/services/api_key_service.go
package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
)

type APIKeyService interface {
	CreateAPIKey(ctx context.Context, userID, email string, req *models.CreateAPIKeyRequest) (*models.CreateAPIKeyResponse, error)
	ValidateAPIKey(ctx context.Context, apiKey string) (*models.APIKey, error)
	GetUserAPIKey(ctx context.Context, userID string) (*models.APIKeyResponse, error) // Changed from GetUserAPIKeys
	UpdateAPIKey(ctx context.Context, userID string, req *models.UpdateAPIKeyRequest) error // Removed keyID param
	RevokeAPIKey(ctx context.Context, userID string) error // Removed keyID param
	UpdateUsage(ctx context.Context, keyHash string) error
	GetAPIKeyStats(ctx context.Context, userID string) (*models.APIKeyStatsResponse, error)
}

type apiKeyService struct {
	apiKeyRepo repository.APIKeyRepository
	userRepo   repository.UserRepository
}

func NewAPIKeyService(apiKeyRepo repository.APIKeyRepository, userRepo repository.UserRepository) APIKeyService {
	return &apiKeyService{
		apiKeyRepo: apiKeyRepo,
		userRepo:   userRepo,
	}
}

func (s *apiKeyService) CreateAPIKey(ctx context.Context, userID, email string, req *models.CreateAPIKeyRequest) (*models.CreateAPIKeyResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Check if user exists
	_, err := s.userRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Delete any existing API key for this user
	if err := s.apiKeyRepo.DeleteByUserID(ctx, userID); err != nil {
		// Log the error but don't fail the creation if no existing key found
		// The error handling in repository should distinguish between "not found" and actual errors
	}

	// Generate API key
	apiKey, keyHash, keyPrefix, err := s.generateAPIKey()
	if err != nil {
		return nil, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			500,
			"failed to generate API key",
			err.Error(),
		)
	}

	// Create API key record
	now := time.Now()
	apiKeyRecord := &models.APIKey{
		UserID:      userID,
		Email:       email,
		KeyName:     req.KeyName,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		IsActive:    true,
		UsageCount:  0,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   req.ExpiresAt,
	}

	if err := s.apiKeyRepo.Create(ctx, apiKeyRecord); err != nil {
		return nil, err
	}

	return &models.CreateAPIKeyResponse{
		Message:   "API key created successfully",
		APIKey:    apiKey, // Return full key only once
		KeyName:   req.KeyName,
		KeyPrefix: keyPrefix,
		ExpiresAt: req.ExpiresAt,
		CreatedAt: now,
	}, nil
}

func (s *apiKeyService) ValidateAPIKey(ctx context.Context, apiKey string) (*models.APIKey, error) {
	// Hash the provided API key
	keyHash := s.hashAPIKey(apiKey)
	
	// Get active API key from database
	apiKeyRecord, err := s.apiKeyRepo.GetActiveByHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}

	// Update last used timestamp asynchronously
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.apiKeyRepo.UpdateLastUsed(bgCtx, keyHash)
	}()

	return apiKeyRecord, nil
}

// GetUserAPIKey returns the single API key for a user (changed from GetUserAPIKeys)
func (s *apiKeyService) GetUserAPIKey(ctx context.Context, userID string) (*models.APIKeyResponse, error) {
	apiKey, err := s.apiKeyRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Sanitize the key (remove sensitive data)
	sanitizedKey := apiKey.Sanitize()

	return &models.APIKeyResponse{
		Message: "API key retrieved successfully",
		Key:     &sanitizedKey,
	}, nil
}

// UpdateAPIKey updates the user's single API key (removed keyID parameter)
func (s *apiKeyService) UpdateAPIKey(ctx context.Context, userID string, req *models.UpdateAPIKeyRequest) error {
	// Validate request
	if err := req.Validate(); err != nil {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	// Get existing key to verify ownership and get ID
	existingKey, err := s.apiKeyRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	// Build update document
	update := bson.M{}
	if req.KeyName != "" {
		update["keyName"] = req.KeyName
	}
	if req.IsActive != nil {
		update["isActive"] = *req.IsActive
	}

	if len(update) == 0 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "no fields to update")
	}

	return s.apiKeyRepo.Update(ctx, existingKey.ID, update)
}

// RevokeAPIKey revokes the user's single API key (removed keyID parameter)
func (s *apiKeyService) RevokeAPIKey(ctx context.Context, userID string) error {
	return s.apiKeyRepo.DeleteByUserID(ctx, userID)
}

func (s *apiKeyService) UpdateUsage(ctx context.Context, keyHash string) error {
	return s.apiKeyRepo.UpdateLastUsed(ctx, keyHash)
}

// GetAPIKeyStats returns statistics for user's API key (now singular)
func (s *apiKeyService) GetAPIKeyStats(ctx context.Context, userID string) (*models.APIKeyStatsResponse, error) {
	// Get user's API key
	apiKey, err := s.apiKeyRepo.GetByUserID(ctx, userID)
	if err != nil {
		// If no API key found, return zero stats
		// Check if it's a "not found" error by examining the error type or message
		if err.Error() == "api key not found" || err.Error() == "no documents in result" {
			return &models.APIKeyStatsResponse{
				Message: "API key statistics retrieved successfully",
				Stats: models.APIKeyStats{
					TotalKeys:    0,
					ActiveKeys:   0,
					InactiveKeys: 0,
					ExpiredKeys:  0,
					TotalUsage:   0,
					LastUsedAt:   nil,
					OldestKeyAt:  nil,
					NewestKeyAt:  nil,
				},
			}, nil
		}
		return nil, err
	}

	now := time.Now()
	
	// Calculate stats for the single key
	totalKeys := 1
	activeKeys := 0
	expiredKeys := 0
	
	if apiKey.IsActive {
		activeKeys = 1
	}
	
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(now) {
		expiredKeys = 1
	}
	
	inactiveKeys := totalKeys - activeKeys

	return &models.APIKeyStatsResponse{
		Message: "API key statistics retrieved successfully",
		Stats: models.APIKeyStats{
			TotalKeys:    totalKeys,
			ActiveKeys:   activeKeys,
			InactiveKeys: inactiveKeys,
			ExpiredKeys:  expiredKeys,
			TotalUsage:   apiKey.UsageCount,
			LastUsedAt:   apiKey.LastUsedAt,
			OldestKeyAt:  &apiKey.CreatedAt,
			NewestKeyAt:  &apiKey.CreatedAt,
		},
	}, nil
}

// generateAPIKey creates a new API key with format: ak_live_<32_random_chars>
func (s *apiKeyService) generateAPIKey() (apiKey, keyHash, keyPrefix string, err error) {
	// Generate 32 random bytes
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", err
	}

	// Convert to hex string
	randomHex := hex.EncodeToString(randomBytes)
	
	// Create API key with prefix
	apiKey = fmt.Sprintf("ak_live_%s", randomHex)
	
	// Hash the API key for storage
	keyHash = s.hashAPIKey(apiKey)
	
	// Get prefix for identification (first 12 chars after ak_live_)
	keyPrefix = fmt.Sprintf("ak_live_%s", randomHex[:8])
	
	return apiKey, keyHash, keyPrefix, nil
}

// hashAPIKey creates a SHA-256 hash of the API key
func (s *apiKeyService) hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}