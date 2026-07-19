package services

import (
	"context"
	"testing"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

func TestDeductCreditsReturnsBalanceFromAtomicUpdate(t *testing.T) {
	repo := &fakeCreditsRepo{deductResult: &models.Credits{UserID: "user-1", Credits: 7}}
	svc := NewCreditsService(repo, nil)

	resp, err := svc.DeductCredits(context.Background(), &models.DeductCreditsRequest{UserID: "user-1", Amount: 3})
	if err != nil {
		t.Fatalf("DeductCredits: %v", err)
	}
	if resp.Credits != 7 {
		t.Fatalf("Credits = %d, want 7 (balance returned by atomic update)", resp.Credits)
	}
	if repo.deductCalls != 1 {
		t.Fatalf("repo DeductCredits calls = %d, want 1", repo.deductCalls)
	}
	if repo.getByUserIDCalls != 0 {
		t.Fatalf("repo GetByUserID calls = %d, want 0 (no read-then-write)", repo.getByUserIDCalls)
	}
}

func TestDeductCreditsPropagatesInsufficientCredits(t *testing.T) {
	repo := &fakeCreditsRepo{deductErr: apperrors.NewInsufficientCreditsError()}
	svc := NewCreditsService(repo, nil)

	_, err := svc.DeductCredits(context.Background(), &models.DeductCreditsRequest{UserID: "user-1", Amount: 3})
	if !apperrors.IsErrorType(err, apperrors.ErrInsufficientCredits) {
		t.Fatalf("err = %v, want INSUFFICIENT_CREDITS", err)
	}
}

func TestDeductCreditsValidatesRequest(t *testing.T) {
	repo := &fakeCreditsRepo{}
	svc := NewCreditsService(repo, nil)

	if _, err := svc.DeductCredits(context.Background(), &models.DeductCreditsRequest{UserID: "", Amount: 3}); err == nil {
		t.Fatal("expected validation error for missing userId")
	}
	if _, err := svc.DeductCredits(context.Background(), &models.DeductCreditsRequest{UserID: "user-1", Amount: 0}); err == nil {
		t.Fatal("expected validation error for non-positive amount")
	}
	if repo.deductCalls != 0 {
		t.Fatalf("repo DeductCredits calls = %d, want 0", repo.deductCalls)
	}
}

type fakeCreditsRepo struct {
	deductResult     *models.Credits
	deductErr        error
	deductCalls      int
	getByUserIDCalls int
}

func (f *fakeCreditsRepo) Create(ctx context.Context, credits *models.Credits) error {
	return nil
}

func (f *fakeCreditsRepo) GetByUserID(ctx context.Context, userID string) (*models.Credits, error) {
	f.getByUserIDCalls++
	return &models.Credits{UserID: userID, Credits: 0}, nil
}

func (f *fakeCreditsRepo) UpdateCredits(ctx context.Context, userID string, amount int) error {
	return nil
}

func (f *fakeCreditsRepo) UpsertCredits(ctx context.Context, userID string, amount int) (*models.Credits, error) {
	return &models.Credits{UserID: userID, Credits: amount}, nil
}

func (f *fakeCreditsRepo) DeductCredits(ctx context.Context, userID string, amount int) (*models.Credits, error) {
	f.deductCalls++
	if f.deductErr != nil {
		return nil, f.deductErr
	}
	return f.deductResult, nil
}

func (f *fakeCreditsRepo) DeleteByUserID(ctx context.Context, userID string) error {
	return nil
}

func (f *fakeCreditsRepo) GetTotalCredits(ctx context.Context) (int64, error) {
	return 0, nil
}

func (f *fakeCreditsRepo) GetAllWithUsers(ctx context.Context) ([]models.AdminUser, error) {
	return nil, nil
}
