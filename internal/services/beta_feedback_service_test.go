package services

import (
	"context"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeBetaFeedbackRepo struct {
	usage models.BetaFeedbackRefundUsage
}

func (f *fakeBetaFeedbackRepo) Create(ctx context.Context, session *models.BetaFeedbackSession) error {
	return nil
}

func (f *fakeBetaFeedbackRepo) GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaFeedbackSession, error) {
	return nil, nil
}

func (f *fakeBetaFeedbackRepo) GetPendingByUserAndService(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSession, error) {
	return nil, nil
}

func (f *fakeBetaFeedbackRepo) SupersedePendingByUserAndService(ctx context.Context, userID, serviceName string) error {
	return nil
}

func (f *fakeBetaFeedbackRepo) SubmitFeedback(ctx context.Context, id primitive.ObjectID, expected *models.BetaFeedbackExpectedResult, refundedCredits int) error {
	return nil
}

func (f *fakeBetaFeedbackRepo) SumRefundedCreditsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	return f.usage.TotalCredits, nil
}

func (f *fakeBetaFeedbackRepo) GetRefundUsageSince(ctx context.Context, userID string, since time.Time) (models.BetaFeedbackRefundUsage, error) {
	return f.usage, nil
}

func (f *fakeBetaFeedbackRepo) List(ctx context.Context, serviceName string, limit int) ([]*models.BetaFeedbackSession, error) {
	return nil, nil
}

type fakeBetaFeedbackUserRepo struct {
	user *models.User
}

func (f *fakeBetaFeedbackUserRepo) Create(ctx context.Context, user *models.User) error { return nil }
func (f *fakeBetaFeedbackUserRepo) GetByUserID(ctx context.Context, userID string) (*models.User, error) {
	return f.user, nil
}
func (f *fakeBetaFeedbackUserRepo) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	return f.user, nil
}
func (f *fakeBetaFeedbackUserRepo) IsSuspendedByEmail(ctx context.Context, email string) (bool, error) {
	return false, nil
}
func (f *fakeBetaFeedbackUserRepo) Delete(ctx context.Context, userID string) error { return nil }
func (f *fakeBetaFeedbackUserRepo) UpdateActiveStatus(ctx context.Context, userID string, isActive bool) error {
	return nil
}
func (f *fakeBetaFeedbackUserRepo) UpdateBillingCurrency(ctx context.Context, userID string, currency string) error {
	return nil
}
func (f *fakeBetaFeedbackUserRepo) ClearBillingCurrency(ctx context.Context, userID string) error {
	return nil
}
func (f *fakeBetaFeedbackUserRepo) UpdateBetaFeedbackRefundCapOverride(ctx context.Context, userID string, cap *int) error {
	f.user.BetaFeedbackMonthlyRefundCapOverride = cap
	return nil
}
func (f *fakeBetaFeedbackUserRepo) SetBetaFeedbackRefundBudgetResetAt(ctx context.Context, userID string, at time.Time) error {
	f.user.BetaFeedbackRefundBudgetResetAt = &at
	return nil
}
func (f *fakeBetaFeedbackUserRepo) GetAll(ctx context.Context) ([]models.User, error) { return nil, nil }
func (f *fakeBetaFeedbackUserRepo) GetTotalCount(ctx context.Context) (int64, error)  { return 0, nil }

func TestEffectiveRefundCapUsesOverride(t *testing.T) {
	override := 20
	user := &models.User{BetaFeedbackMonthlyRefundCapOverride: &override}
	if got := effectiveRefundCap(user); got != 20 {
		t.Fatalf("expected override cap 20, got %d", got)
	}
}

func TestRefundWindowStartUsesResetTimestamp(t *testing.T) {
	resetAt := time.Now().Add(-2 * time.Hour)
	user := &models.User{BetaFeedbackRefundBudgetResetAt: &resetAt}
	got := refundWindowStart(user)
	if got.Before(resetAt.Add(-time.Second)) || got.After(resetAt.Add(time.Second)) {
		t.Fatalf("expected window start near reset timestamp, got %v", got)
	}
}

func TestGetUserRefundBudgetRemaining(t *testing.T) {
	t.Setenv("BETA_FEEDBACK_MONTHLY_REFUND_CAP", "50")
	user := &models.User{UserID: "user@test.com", Email: "user@test.com"}
	svc := NewBetaFeedbackService(
		&fakeBetaFeedbackRepo{usage: models.BetaFeedbackRefundUsage{TotalCredits: 42, SessionCount: 21}},
		&fakeBetaFeedbackUserRepo{user: user},
		nil,
	).(*betaFeedbackService)

	budget, err := svc.GetUserRefundBudget(context.Background(), user.UserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if budget.CreditsUsed != 42 {
		t.Fatalf("expected 42 used, got %d", budget.CreditsUsed)
	}
	if budget.CreditsRemaining != 8 {
		t.Fatalf("expected 8 remaining, got %d", budget.CreditsRemaining)
	}
	if budget.CapExhausted {
		t.Fatal("expected cap not exhausted")
	}
}

func TestRefundableAmountZeroWhenCapExhausted(t *testing.T) {
	t.Setenv("BETA_FEEDBACK_MONTHLY_REFUND_CAP", "50")
	user := &models.User{UserID: "user@test.com", Email: "user@test.com"}
	svc := NewBetaFeedbackService(
		&fakeBetaFeedbackRepo{usage: models.BetaFeedbackRefundUsage{TotalCredits: 50}},
		&fakeBetaFeedbackUserRepo{user: user},
		nil,
	).(*betaFeedbackService)

	amount, err := svc.refundableAmount(context.Background(), user.UserID, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amount != 0 {
		t.Fatalf("expected 0 refund, got %d", amount)
	}
}

func TestResetUserRefundBudgetRequiresConfirm(t *testing.T) {
	user := &models.User{UserID: "user@test.com", Email: "user@test.com"}
	svc := NewBetaFeedbackService(
		&fakeBetaFeedbackRepo{},
		&fakeBetaFeedbackUserRepo{user: user},
		nil,
	)

	_, err := svc.ResetUserRefundBudget(context.Background(), user.UserID, "NOPE")
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestNormalizeExpectedClassificationFailedAlias(t *testing.T) {
	yes := true
	expected := &models.BetaFeedbackExpectedResult{
		ExpectedClassification: "failed",
		ResponseAsExpected:     &yes,
	}
	if err := expected.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if expected.ExpectedClassification != "Not-Detected" {
		t.Fatalf("expected Not-Detected, got %q", expected.ExpectedClassification)
	}
}

func TestResponseAsExpectedFalsePreserved(t *testing.T) {
	no := false
	score := 42.0
	expected := &models.BetaFeedbackExpectedResult{
		ExpectedClassification: "Forged",
		ExpectedSimilarityMin:  &score,
		ExpectedSimilarityMax:  &score,
		ResponseAsExpected:     &no,
	}
	if err := expected.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if expected.ResponseAsExpected == nil || *expected.ResponseAsExpected {
		t.Fatal("expected responseAsExpected=false to be preserved")
	}
}
