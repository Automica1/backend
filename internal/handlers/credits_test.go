package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
)

func newCreditsDeductRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/v1/credits/deduct", bytes.NewBufferString(body))
}

func withAuthContext(r *http.Request, email string, isAdmin bool) *http.Request {
	ctx := context.WithValue(r.Context(), "email", email)
	ctx = context.WithValue(ctx, "isAdmin", isAdmin)
	return r.WithContext(ctx)
}

func TestDeductCreditsRejectsForeignUserForNonAdmin(t *testing.T) {
	credits := &creditsTestService{}
	users := &creditsTestUserService{userID: "caller-1", email: "caller@example.com"}
	handler := NewCreditsHandler(credits, users, nil)

	req := newCreditsDeductRequest(`{"userId":"victim-2","amount":5}`)
	req = withAuthContext(req, "caller@example.com", false)
	rec := httptest.NewRecorder()

	handler.DeductCredits(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if credits.deductCalls != 0 {
		t.Fatalf("DeductCredits calls = %d, want 0", credits.deductCalls)
	}
}

func TestDeductCreditsDerivesUserIDForNonAdmin(t *testing.T) {
	credits := &creditsTestService{balance: 10}
	users := &creditsTestUserService{userID: "caller-1", email: "caller@example.com"}
	handler := NewCreditsHandler(credits, users, nil)

	req := newCreditsDeductRequest(`{"amount":5}`)
	req = withAuthContext(req, "caller@example.com", false)
	rec := httptest.NewRecorder()

	handler.DeductCredits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if credits.lastDeductUserID != "caller-1" {
		t.Fatalf("deducted userId = %q, want caller-1", credits.lastDeductUserID)
	}
}

func TestDeductCreditsAllowsOwnUserIDForNonAdmin(t *testing.T) {
	credits := &creditsTestService{balance: 10}
	users := &creditsTestUserService{userID: "caller-1", email: "caller@example.com"}
	handler := NewCreditsHandler(credits, users, nil)

	req := newCreditsDeductRequest(`{"userId":"caller-1","amount":5}`)
	req = withAuthContext(req, "caller@example.com", false)
	rec := httptest.NewRecorder()

	handler.DeductCredits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if credits.lastDeductUserID != "caller-1" {
		t.Fatalf("deducted userId = %q, want caller-1", credits.lastDeductUserID)
	}
}

func TestDeductCreditsUsesAPIKeyUserIDForNonAdmin(t *testing.T) {
	credits := &creditsTestService{balance: 10}
	users := &creditsTestUserService{} // must not be consulted when context has a user ID
	handler := NewCreditsHandler(credits, users, nil)

	req := newCreditsDeductRequest(`{"amount":5}`)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDContextKey, "apikey-user-1"))
	rec := httptest.NewRecorder()

	handler.DeductCredits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if credits.lastDeductUserID != "apikey-user-1" {
		t.Fatalf("deducted userId = %q, want apikey-user-1", credits.lastDeductUserID)
	}
	if users.getByEmailCalls != 0 {
		t.Fatalf("GetUserByEmail calls = %d, want 0", users.getByEmailCalls)
	}
}

func TestDeductCreditsAllowsForeignUserForAdmin(t *testing.T) {
	credits := &creditsTestService{balance: 10}
	users := &creditsTestUserService{userID: "admin-1", email: "admin@example.com"}
	handler := NewCreditsHandler(credits, users, nil)

	req := newCreditsDeductRequest(`{"userId":"target-2","amount":5}`)
	req = withAuthContext(req, "admin@example.com", true)
	rec := httptest.NewRecorder()

	handler.DeductCredits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if credits.lastDeductUserID != "target-2" {
		t.Fatalf("deducted userId = %q, want target-2", credits.lastDeductUserID)
	}
}

type creditsTestService struct {
	balance          int
	deductCalls      int
	lastDeductUserID string
}

func (s *creditsTestService) GetBalance(ctx context.Context, userID string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: userID, Credits: s.balance}, nil
}

func (s *creditsTestService) GetBalanceByEmail(ctx context.Context, email string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: email, Credits: s.balance}, nil
}

func (s *creditsTestService) AddCredits(ctx context.Context, req *models.AddCreditsRequest) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: req.UserID, Credits: s.balance + req.Amount}, nil
}

func (s *creditsTestService) DeductCredits(ctx context.Context, req *models.DeductCreditsRequest) (*models.CreditsResponse, error) {
	s.deductCalls++
	s.lastDeductUserID = req.UserID
	return &models.CreditsResponse{UserID: req.UserID, Credits: s.balance - req.Amount}, nil
}

type creditsTestUserService struct {
	userID          string
	email           string
	getByEmailCalls int
}

func (s *creditsTestUserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	s.getByEmailCalls++
	if email != s.email {
		return nil, errors.New("user not found")
	}
	return &models.User{UserID: s.userID, Email: s.email}, nil
}

func (s *creditsTestUserService) RegisterUser(ctx context.Context, req *models.RegisterUserRequest) (*models.RegisterUserResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) GetOrCreateUser(ctx context.Context, email string) (*models.User, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) GetAllUsers(ctx context.Context) (*models.AdminUserListResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) ListUsers(ctx context.Context, query models.AdminListQuery) (*models.AdminUserListResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) GetUserByID(ctx context.Context, userID string) (*models.AdminUserDetailResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) GetUserStats(ctx context.Context) (*models.UserStatsResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) GetUserActivity(ctx context.Context, userID string) (*models.UserActivityResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) GetUserCredits(ctx context.Context, userID string) (*models.UserCreditsResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *creditsTestUserService) DeleteUser(ctx context.Context, userID string) error {
	return errors.New("not implemented")
}

func (s *creditsTestUserService) SuspendUser(ctx context.Context, userID string) error {
	return errors.New("not implemented")
}

func (s *creditsTestUserService) ReactivateUser(ctx context.Context, userID string) error {
	return errors.New("not implemented")
}

func (s *creditsTestUserService) SetBillingCurrency(ctx context.Context, userID, currency string) error {
	return errors.New("not implemented")
}

func (s *creditsTestUserService) ClearBillingCurrency(ctx context.Context, userID string) error {
	return errors.New("not implemented")
}
