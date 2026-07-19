package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type guestPassTestService struct {
	pass *models.GuestPass
}

func (s *guestPassTestService) CreatePass(ctx context.Context, req *models.CreateGuestPassRequest, createdBy string) (*models.CreateGuestPassResponse, error) {
	return nil, nil
}

func (s *guestPassTestService) ValidateKey(ctx context.Context, plaintextKey string) (*models.GuestPass, error) {
	if s.pass == nil {
		return nil, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "invalid or expired guest pass")
	}
	return s.pass, nil
}

func (s *guestPassTestService) ValidateKeyForService(ctx context.Context, plaintextKey, serviceSlug string) (*models.GuestPass, error) {
	return s.ValidateKey(ctx, plaintextKey)
}

func (s *guestPassTestService) GetBalance(ctx context.Context, plaintextKey, serviceSlug string) (*models.GuestPassBalanceResponse, error) {
	return &models.GuestPassBalanceResponse{RemainingCredits: 42, AllowedServices: []string{"qr-mask"}}, nil
}

func (s *guestPassTestService) ListPasses(ctx context.Context) ([]*models.GuestPass, error) {
	return nil, nil
}

func (s *guestPassTestService) UpdatePass(ctx context.Context, passID string, req *models.UpdateGuestPassRequest) (*models.GuestPass, error) {
	return nil, nil
}

func (s *guestPassTestService) RevokePass(ctx context.Context, passID string) (*models.RevokeGuestPassResponse, error) {
	return nil, nil
}

func (s *guestPassTestService) SyncBalanceAfterDeduction(ctx context.Context, passID primitive.ObjectID, walletUserID string) error {
	return nil
}

func (s *guestPassTestService) RecordUsage(ctx context.Context, keyHash string) error {
	return nil
}

func (s *guestPassTestService) ListSupportedServices() []string {
	return models.GuestPassServiceSlugs
}

func callValidatePass(t *testing.T, svc *guestPassTestService, body string) map[string]interface{} {
	t.Helper()
	handler := NewGuestPassHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/guest-passes/validate", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ValidatePass(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	return payload
}

func TestValidatePassPublicResponseIsSanitized(t *testing.T) {
	svc := &guestPassTestService{pass: &models.GuestPass{
		IsActive:         true,
		RemainingCredits: 42,
		AllowedServices:  []string{"qr-mask"},
	}}

	payload := callValidatePass(t, svc, `{"key":"apple-berry-cedar-abcd2345-efgh6789","serviceSlug":"qr-mask"}`)

	if payload["valid"] != true {
		t.Fatalf("valid = %v, want true", payload["valid"])
	}
	if payload["serviceAllowed"] != true {
		t.Fatalf("serviceAllowed = %v, want true", payload["serviceAllowed"])
	}
	for _, forbidden := range []string{"remainingCredits", "allowedServices", "label", "expiresAt"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("public validate response must not expose %q; body=%v", forbidden, payload)
		}
	}
}

func TestValidatePassReportsServiceNotAllowed(t *testing.T) {
	svc := &guestPassTestService{pass: &models.GuestPass{
		IsActive:        true,
		AllowedServices: []string{"qr-mask"},
	}}

	payload := callValidatePass(t, svc, `{"key":"apple-berry-cedar-abcd2345-efgh6789","serviceSlug":"signature-verification"}`)

	if payload["valid"] != true {
		t.Fatalf("valid = %v, want true", payload["valid"])
	}
	if payload["serviceAllowed"] != false {
		t.Fatalf("serviceAllowed = %v, want false", payload["serviceAllowed"])
	}
}

func TestValidatePassInvalidKeyIsGeneric(t *testing.T) {
	payload := callValidatePass(t, &guestPassTestService{pass: nil}, `{"key":"wrong-key"}`)

	if payload["valid"] != false {
		t.Fatalf("valid = %v, want false", payload["valid"])
	}
	for _, forbidden := range []string{"remainingCredits", "allowedServices"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("invalid-key response must not expose %q; body=%v", forbidden, payload)
		}
	}
}
