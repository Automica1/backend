package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type GuestPassService interface {
	CreatePass(ctx context.Context, req *models.CreateGuestPassRequest, createdBy string) (*models.CreateGuestPassResponse, error)
	ValidateKey(ctx context.Context, plaintextKey string) (*models.GuestPass, error)
	ValidateKeyForService(ctx context.Context, plaintextKey, serviceSlug string) (*models.GuestPass, error)
	GetBalance(ctx context.Context, plaintextKey, serviceSlug string) (*models.GuestPassBalanceResponse, error)
	ListPasses(ctx context.Context) ([]*models.GuestPass, error)
	UpdatePass(ctx context.Context, passID string, req *models.UpdateGuestPassRequest) (*models.GuestPass, error)
	RevokePass(ctx context.Context, passID string) (*models.RevokeGuestPassResponse, error)
	SyncBalanceAfterDeduction(ctx context.Context, passID primitive.ObjectID, walletUserID string) error
	RecordUsage(ctx context.Context, keyHash string) error
	ListSupportedServices() []string
}

type guestPassService struct {
	guestPassRepo repository.GuestPassRepository
	creditsRepo   repository.CreditsRepository
}

func NewGuestPassService(guestPassRepo repository.GuestPassRepository, creditsRepo repository.CreditsRepository) GuestPassService {
	return &guestPassService{
		guestPassRepo: guestPassRepo,
		creditsRepo:   creditsRepo,
	}
}

var guestPassWords = []string{
	"apple", "arrow", "amber", "anchor", "apricot", "atlas", "azure", "banana", "beacon", "berry",
	"blaze", "bloom", "breeze", "bronze", "canyon", "carbon", "cedar", "cherry", "citrus", "cloud",
	"cobalt", "comet", "coral", "cosmic", "cotton", "crimson", "crystal", "daisy", "delta", "diamond",
	"dragon", "eagle", "ember", "falcon", "fern", "flame", "forest", "frost", "galaxy", "garden",
	"ginger", "glacier", "golden", "granite", "harbor", "hazel", "horizon", "island", "ivory", "jade",
	"jasmine", "jupiter", "kernel", "kitten", "lagoon", "lemon", "lilac", "lotus", "maple", "marble",
	"meadow", "mercury", "meteor", "mint", "mirror", "monarch", "moon", "mountain", "nectar", "nebula",
	"noble", "ocean", "olive", "onyx", "orange", "orchid", "orbit", "otter", "panda", "peach",
	"pearl", "pebble", "phoenix", "pilot", "pine", "planet", "plasma", "plum", "prairie", "quartz",
	"rabbit", "radar", "rain", "raven", "river", "robin", "rocket", "rose", "ruby", "saffron",
	"sage", "sapphire", "shadow", "silver", "sky", "snow", "solar", "spark", "spring", "star",
	"stone", "storm", "sun", "sunset", "tiger", "topaz", "tower", "tulip", "turbo", "valley",
	"velvet", "violet", "vista", "wave", "willow", "wind", "winter", "wolf", "wonder", "zenith",
}

func (s *guestPassService) ListSupportedServices() []string {
	return models.GuestPassServiceSlugs
}

func (s *guestPassService) CreatePass(ctx context.Context, req *models.CreateGuestPassRequest, createdBy string) (*models.CreateGuestPassResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	plaintext, keyHash, keyPrefix, err := s.generateGuestPassKey()
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to generate guest pass key")
	}

	now := time.Now()
	var expiresAt *time.Time
	if req.ExpiresInDays != nil {
		expiry := now.Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &expiry
	}

	pass := &models.GuestPass{
		KeyHash:          keyHash,
		KeyPrefix:        keyPrefix,
		Label:            req.Label,
		Description:      req.Description,
		InitialCredits:   req.Credits,
		RemainingCredits: req.Credits,
		AllowedServices:  req.AllowedServices,
		CreatedBy:        createdBy,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        expiresAt,
		IsActive:         true,
		UsageCount:       0,
	}

	if err := s.guestPassRepo.Create(ctx, pass); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to store guest pass")
	}

	pass.WalletUserID = fmt.Sprintf("guestpass_%s", pass.ID.Hex())
	if err := s.guestPassRepo.Update(ctx, pass.ID, bson.M{"walletUserId": pass.WalletUserID}); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to finalize guest pass wallet")
	}

	if err := s.creditsRepo.Create(ctx, &models.Credits{
		UserID:  pass.WalletUserID,
		Credits: req.Credits,
	}); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create guest pass credits wallet")
	}

	return &models.CreateGuestPassResponse{
		Message:         "Guest pass created successfully",
		GuestPassKey:    plaintext,
		KeyPrefix:       keyPrefix,
		Label:           req.Label,
		InitialCredits:  req.Credits,
		AllowedServices: req.AllowedServices,
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
	}, nil
}

func (s *guestPassService) ValidateKey(ctx context.Context, plaintextKey string) (*models.GuestPass, error) {
	normalized := models.NormalizeGuestPassKey(plaintextKey)
	if normalized == "" {
		return nil, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "guest pass key is required")
	}

	keyHash := s.hashGuestPassKey(normalized)
	pass, err := s.guestPassRepo.GetActiveByHash(ctx, keyHash)
	if err != nil {
		return nil, err
	}
	if !pass.IsValid() {
		return nil, apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "invalid or expired guest pass")
	}
	return pass, nil
}

func (s *guestPassService) ValidateKeyForService(ctx context.Context, plaintextKey, serviceSlug string) (*models.GuestPass, error) {
	pass, err := s.ValidateKey(ctx, plaintextKey)
	if err != nil {
		return nil, err
	}
	if serviceSlug != "" && !pass.AllowsService(serviceSlug) {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "guest pass is not valid for this service")
	}
	return pass, nil
}

func (s *guestPassService) GetBalance(ctx context.Context, plaintextKey, serviceSlug string) (*models.GuestPassBalanceResponse, error) {
	pass, err := s.ValidateKey(ctx, plaintextKey)
	if err != nil {
		return nil, err
	}

	balance, err := s.creditsRepo.GetByUserID(ctx, pass.WalletUserID)
	if err != nil {
		return nil, err
	}

	_ = s.guestPassRepo.SyncRemainingCredits(ctx, pass.ID, balance.Credits)

	serviceAllowed := serviceSlug == "" || pass.AllowsService(serviceSlug)
	return &models.GuestPassBalanceResponse{
		Message:          "Guest pass balance retrieved successfully",
		Label:            pass.Label,
		RemainingCredits: balance.Credits,
		AllowedServices:  pass.AllowedServices,
		ExpiresAt:        pass.ExpiresAt,
		ServiceAllowed:   serviceAllowed,
	}, nil
}

func (s *guestPassService) ListPasses(ctx context.Context) ([]*models.GuestPass, error) {
	passes, err := s.guestPassRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, pass := range passes {
		if pass.WalletUserID == "" {
			continue
		}
		balance, err := s.creditsRepo.GetByUserID(ctx, pass.WalletUserID)
		if err == nil && balance != nil {
			pass.RemainingCredits = balance.Credits
		}
	}
	return passes, nil
}

func (s *guestPassService) UpdatePass(ctx context.Context, passID string, req *models.UpdateGuestPassRequest) (*models.GuestPass, error) {
	if err := req.Validate(); err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "validation failed", err.Error())
	}

	objectID, err := primitive.ObjectIDFromHex(passID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid guest pass id")
	}

	pass, err := s.guestPassRepo.GetByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	update := bson.M{}
	if req.Label != nil {
		update["label"] = *req.Label
	}
	if req.Description != nil {
		update["description"] = *req.Description
	}
	if req.AllowedServices != nil {
		update["allowedServices"] = req.AllowedServices
	}
	if req.ExpiresInDays != nil {
		expiry := time.Now().Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour)
		update["expiresAt"] = expiry
	}
	if req.TopUpCredits != nil && *req.TopUpCredits > 0 {
		if _, err := s.creditsRepo.UpsertCredits(ctx, pass.WalletUserID, *req.TopUpCredits); err != nil {
			return nil, err
		}
		pass.InitialCredits += *req.TopUpCredits
		update["initialCredits"] = pass.InitialCredits
	}

	if len(update) > 0 {
		if err := s.guestPassRepo.Update(ctx, objectID, update); err != nil {
			return nil, err
		}
	}

	updated, err := s.guestPassRepo.GetByID(ctx, objectID)
	if err != nil {
		return nil, err
	}
	if balance, err := s.creditsRepo.GetByUserID(ctx, updated.WalletUserID); err == nil {
		updated.RemainingCredits = balance.Credits
		_ = s.guestPassRepo.SyncRemainingCredits(ctx, objectID, balance.Credits)
	}
	return updated, nil
}

func (s *guestPassService) RevokePass(ctx context.Context, passID string) (*models.RevokeGuestPassResponse, error) {
	objectID, err := primitive.ObjectIDFromHex(passID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid guest pass id")
	}
	if err := s.guestPassRepo.Revoke(ctx, objectID); err != nil {
		return nil, err
	}
	return &models.RevokeGuestPassResponse{
		Message: "Guest pass revoked successfully",
		ID:      passID,
	}, nil
}

func (s *guestPassService) SyncBalanceAfterDeduction(ctx context.Context, passID primitive.ObjectID, walletUserID string) error {
	balance, err := s.creditsRepo.GetByUserID(ctx, walletUserID)
	if err != nil {
		return err
	}
	return s.guestPassRepo.SyncRemainingCredits(ctx, passID, balance.Credits)
}

func (s *guestPassService) RecordUsage(ctx context.Context, keyHash string) error {
	return s.guestPassRepo.UpdateLastUsed(ctx, keyHash)
}

func (s *guestPassService) generateGuestPassKey() (plaintext, keyHash, keyPrefix string, err error) {
	w1, err := randomGuestPassWord()
	if err != nil {
		return "", "", "", err
	}
	w2, err := randomGuestPassWord()
	if err != nil {
		return "", "", "", err
	}
	w3, err := randomGuestPassWord()
	if err != nil {
		return "", "", "", err
	}

	n, err := rand.Int(rand.Reader, big.NewInt(90))
	if err != nil {
		return "", "", "", err
	}
	suffix := int(n.Int64()) + 10

	plaintext = fmt.Sprintf("%s-%s-%s-%d", w1, w2, w3, suffix)
	keyHash = s.hashGuestPassKey(plaintext)
	parts := strings.Split(plaintext, "-")
	if len(parts) >= 2 {
		keyPrefix = fmt.Sprintf("%s-%s-…", parts[0], parts[1])
	} else {
		keyPrefix = plaintext[:min(8, len(plaintext))] + "…"
	}
	return plaintext, keyHash, keyPrefix, nil
}

func randomGuestPassWord() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(guestPassWords))))
	if err != nil {
		return "", err
	}
	return guestPassWords[n.Int64()], nil
}

func (s *guestPassService) hashGuestPassKey(key string) string {
	normalized := models.NormalizeGuestPassKey(key)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
