package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BetaKeyService interface {
	GenerateKey(ctx context.Context, req *models.GenerateBetaKeyRequest, createdBy string) (*models.GenerateBetaKeyResponse, error)
	ValidateKey(ctx context.Context, serviceName, plaintextKey string) (*models.BetaKey, error)
	ListKeys(ctx context.Context, serviceName string) ([]*models.BetaKey, error)
	RevokeKey(ctx context.Context, keyID string) (*models.RevokeBetaKeyResponse, error)
}

type betaKeyService struct {
	betaKeyRepo repository.BetaKeyRepository
}

func NewBetaKeyService(betaKeyRepo repository.BetaKeyRepository) BetaKeyService {
	return &betaKeyService{betaKeyRepo: betaKeyRepo}
}

func (s *betaKeyService) GenerateKey(ctx context.Context, req *models.GenerateBetaKeyRequest, createdBy string) (*models.GenerateBetaKeyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	plaintext, keyHash, keyPrefix, err := s.generateBetaKey()
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to generate beta key")
	}

	now := time.Now()
	var expiresAt *time.Time
	if req.ExpiresInDays != nil {
		expiry := now.Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &expiry
	}

	record := &models.BetaKey{
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		ServiceName: req.ServiceName,
		Label:       req.Label,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		IsActive:    true,
		UsageCount:  0,
	}

	if err := s.betaKeyRepo.Create(ctx, record); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to store beta key")
	}

	return &models.GenerateBetaKeyResponse{
		Message:     "Beta key generated successfully",
		BetaKey:     plaintext,
		KeyPrefix:   keyPrefix,
		ServiceName: req.ServiceName,
		Label:       req.Label,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	}, nil
}

func (s *betaKeyService) ValidateKey(ctx context.Context, serviceName, plaintextKey string) (*models.BetaKey, error) {
	plaintextKey = strings.TrimSpace(plaintextKey)
	if plaintextKey == "" {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "beta key is required")
	}
	if !strings.HasPrefix(plaintextKey, "bk_live_") {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "invalid beta key format")
	}
	if !models.IsBetaServiceSupported(serviceName) {
		return nil, apperrors.NewAppError(apperrors.ErrBadRequest, 400, "beta is not supported for this service")
	}

	keyHash := s.hashBetaKey(plaintextKey)
	record, err := s.betaKeyRepo.GetActiveByHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}
	if record.ServiceName != serviceName {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "beta key is not valid for this service")
	}

	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.betaKeyRepo.UpdateLastUsed(updateCtx, keyHash)
	}()

	return record, nil
}

func (s *betaKeyService) ListKeys(ctx context.Context, serviceName string) ([]*models.BetaKey, error) {
	return s.betaKeyRepo.List(ctx, serviceName)
}

func (s *betaKeyService) RevokeKey(ctx context.Context, keyID string) (*models.RevokeBetaKeyResponse, error) {
	objectID, err := primitive.ObjectIDFromHex(keyID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid beta key id")
	}

	if err := s.betaKeyRepo.Revoke(ctx, objectID); err != nil {
		return nil, err
	}

	return &models.RevokeBetaKeyResponse{
		Message: "Beta key revoked successfully",
		ID:      keyID,
	}, nil
}

func (s *betaKeyService) generateBetaKey() (plaintext, keyHash, keyPrefix string, err error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", err
	}

	randomHex := hex.EncodeToString(randomBytes)
	plaintext = fmt.Sprintf("bk_live_%s", randomHex)
	keyHash = s.hashBetaKey(plaintext)
	keyPrefix = fmt.Sprintf("bk_live_%s", randomHex[:8])
	return plaintext, keyHash, keyPrefix, nil
}

func (s *betaKeyService) hashBetaKey(betaKey string) string {
	hash := sha256.Sum256([]byte(betaKey))
	return hex.EncodeToString(hash[:])
}
