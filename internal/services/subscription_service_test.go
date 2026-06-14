package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"
)

type fakeRazorpayGateway struct {
	cancelCalls []cancelCall
	cancelErr   error
	fetchResult map[string]interface{}
	fetchErr    error
}

type cancelCall struct {
	subscriptionID string
	data           map[string]interface{}
}

func (f *fakeRazorpayGateway) CreateCustomer(data map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"id": "cust_test"}, nil
}

func (f *fakeRazorpayGateway) CreateOrder(data map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"id": "order_test"}, nil
}

func (f *fakeRazorpayGateway) CreateSubscription(data map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"id": "sub_test"}, nil
}

func (f *fakeRazorpayGateway) CancelSubscription(subscriptionID string, data map[string]interface{}) (map[string]interface{}, error) {
	callData := map[string]interface{}{}
	for k, v := range data {
		callData[k] = v
	}
	f.cancelCalls = append(f.cancelCalls, cancelCall{
		subscriptionID: subscriptionID,
		data:           callData,
	})

	if f.cancelErr != nil {
		return nil, f.cancelErr
	}

	return map[string]interface{}{"id": subscriptionID}, nil
}

func (f *fakeRazorpayGateway) FetchSubscription(subscriptionID string) (map[string]interface{}, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	if f.fetchResult != nil {
		return f.fetchResult, nil
	}
	return map[string]interface{}{
		"id":                  subscriptionID,
		"status":              "active",
		"cancel_at_cycle_end": false,
		"current_start":       float64(time.Now().AddDate(0, -1, 0).Unix()),
		"current_end":         float64(time.Now().AddDate(0, 0, 15).Unix()),
	}, nil
}

type fakeEmailService struct {
	cancellationCalls []cancellationCall
}

type cancellationCall struct {
	toEmail     string
	toName      string
	accessUntil time.Time
}

func (f *fakeEmailService) SendSubscriptionConfirmation(toEmail, toName, planName string, credits int, nextBillingDate time.Time) error {
	return nil
}

func (f *fakeEmailService) SendPaymentFailed(toEmail, toName string, retryDate time.Time) error {
	return nil
}

func (f *fakeEmailService) SendCancellationConfirmation(toEmail, toName string, accessUntil time.Time) error {
	f.cancellationCalls = append(f.cancellationCalls, cancellationCall{
		toEmail:     toEmail,
		toName:      toName,
		accessUntil: accessUntil,
	})
	return nil
}

func (f *fakeEmailService) SendSubscriptionExpired(toEmail, toName string) error {
	return nil
}

type fakeSubscriptionRepo struct {
	subs map[string]*models.Subscription
}

func newFakeSubscriptionRepo(subs ...*models.Subscription) *fakeSubscriptionRepo {
	repo := &fakeSubscriptionRepo{subs: map[string]*models.Subscription{}}
	for _, sub := range subs {
		copied := *sub
		repo.subs[sub.SubscriptionID] = &copied
	}
	return repo
}

func (r *fakeSubscriptionRepo) Create(ctx context.Context, sub *models.Subscription) error {
	copied := *sub
	r.subs[sub.SubscriptionID] = &copied
	return nil
}

func (r *fakeSubscriptionRepo) GetByUserID(ctx context.Context, userID string) (*models.Subscription, error) {
	var latest *models.Subscription
	for _, sub := range r.subs {
		if sub.UserID != userID {
			continue
		}
		if latest == nil || sub.UpdatedAt.After(latest.UpdatedAt) {
			copied := *sub
			latest = &copied
		}
	}
	if latest == nil {
		return nil, errors.New("subscription not found for user")
	}
	return latest, nil
}

func (r *fakeSubscriptionRepo) GetByUserIDAndStatus(ctx context.Context, userID string, status models.SubscriptionStatus) (*models.Subscription, error) {
	var latest *models.Subscription
	for _, sub := range r.subs {
		if sub.UserID != userID || sub.Status != status {
			continue
		}
		if latest == nil || sub.UpdatedAt.After(latest.UpdatedAt) {
			copied := *sub
			latest = &copied
		}
	}
	if latest == nil {
		return nil, errors.New("subscription not found by status")
	}
	return latest, nil
}

func (r *fakeSubscriptionRepo) GetBySubscriptionID(ctx context.Context, subID string) (*models.Subscription, error) {
	sub, ok := r.subs[subID]
	if !ok {
		return nil, errors.New("subscription not found")
	}
	copied := *sub
	return &copied, nil
}

func (r *fakeSubscriptionRepo) List(ctx context.Context, query models.AdminSubscriptionQuery) ([]models.Subscription, int64, error) {
	result := make([]models.Subscription, 0)
	for _, sub := range r.subs {
		matches := true
		if query.Status != "" && string(sub.Status) != query.Status {
			matches = false
		}
		if matches && query.UserID != "" && sub.UserID != query.UserID {
			matches = false
		}
		if matches && query.Email != "" && sub.Email != query.Email {
			matches = false
		}
		if matches && query.PlanID != "" && sub.PlanID != query.PlanID {
			matches = false
		}
		if matches && query.SubscriptionID != "" && !strings.Contains(sub.SubscriptionID, query.SubscriptionID) {
			matches = false
		}
		if matches && query.Search != "" {
			needle := strings.ToLower(query.Search)
			matches = strings.Contains(strings.ToLower(sub.UserID), needle) ||
				strings.Contains(strings.ToLower(sub.Email), needle) ||
				strings.Contains(strings.ToLower(sub.PlanID), needle) ||
				strings.Contains(strings.ToLower(sub.SubscriptionID), needle) ||
				strings.Contains(strings.ToLower(string(sub.Status)), needle)
		}
		if matches {
			result = append(result, *sub)
		}
	}
	return result, int64(len(result)), nil
}

func (r *fakeSubscriptionRepo) Update(ctx context.Context, sub *models.Subscription) error {
	copied := *sub
	r.subs[sub.SubscriptionID] = &copied
	return nil
}

func (r *fakeSubscriptionRepo) UpdateStatus(ctx context.Context, subID string, status models.SubscriptionStatus) error {
	sub, ok := r.subs[subID]
	if !ok {
		return errors.New("subscription not found")
	}
	sub.Status = status
	sub.UpdatedAt = time.Now()
	return nil
}

func (r *fakeSubscriptionRepo) GetAll(ctx context.Context) ([]models.Subscription, error) {
	result := make([]models.Subscription, 0, len(r.subs))
	for _, sub := range r.subs {
		result = append(result, *sub)
	}
	return result, nil
}

func (r *fakeSubscriptionRepo) CountActive(ctx context.Context) (int64, error) {
	var count int64
	for _, sub := range r.subs {
		if sub.Status == models.SubscriptionStatusActive {
			count++
		}
	}
	return count, nil
}

func newTestSubscriptionService(repo *fakeSubscriptionRepo, gateway RazorpayGateway, emailSvc EmailService) *subscriptionService {
	return &subscriptionService{
		subRepo:      repo,
		emailService: emailSvc,
		razorpay:     gateway,
	}
}

func TestCancelSubscriptionSchedulesRazorpayCycleEndCancellation(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID:     "sub_123",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		Amount:             19900,
		Currency:           "USD",
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 10),
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	})
	gateway := &fakeRazorpayGateway{}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.CancelSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("cancel subscription returned error: %v", err)
	}

	if len(gateway.cancelCalls) != 1 {
		t.Fatalf("expected 1 Razorpay cancel call, got %d", len(gateway.cancelCalls))
	}
	if gateway.cancelCalls[0].subscriptionID != "sub_123" {
		t.Fatalf("expected subscription ID sub_123, got %s", gateway.cancelCalls[0].subscriptionID)
	}
	if got := gateway.cancelCalls[0].data["cancel_at_cycle_end"]; got != 1 {
		t.Fatalf("expected cancel_at_cycle_end=1, got %#v", got)
	}

	updated := repo.subs["sub_123"]
	if !updated.CancelAtCycleEnd {
		t.Fatalf("expected CancelAtCycleEnd to be true")
	}
	if updated.CancelScheduledAt == nil {
		t.Fatalf("expected CancelScheduledAt to be set")
	}
	if updated.Status != models.SubscriptionStatusActive {
		t.Fatalf("expected status to remain active, got %s", updated.Status)
	}
}

func TestCancelSubscriptionRejectsInactiveSubscriptions(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID:     "sub_456",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusCancelled,
		PlanID:             "pro",
		Amount:             19900,
		Currency:           "USD",
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 10),
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	})
	gateway := &fakeRazorpayGateway{}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	err := svc.CancelSubscription(context.Background(), "user@example.com")
	if err == nil {
		t.Fatal("expected error when cancelling inactive subscription")
	}
	if !strings.Contains(err.Error(), "no active subscription to cancel") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gateway.cancelCalls) != 0 {
		t.Fatalf("expected no Razorpay cancel calls, got %d", len(gateway.cancelCalls))
	}
}

func TestReconcileAdminSubscriptionUpdatesLocalState(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID:     "sub_789",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusCreated,
		PlanID:             "pro",
		Amount:             19900,
		Currency:           "USD",
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 5),
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	})
	gateway := &fakeRazorpayGateway{
		fetchResult: map[string]interface{}{
			"id":                  "sub_789",
			"status":              "active",
			"cancel_at_cycle_end": 1,
			"current_start":       float64(now.AddDate(0, -1, 0).Unix()),
			"current_end":         float64(now.AddDate(0, 0, 20).Unix()),
		},
	}
	svc := &subscriptionService{
		subRepo:     repo,
		razorpay:    gateway,
		planService: &fakePlanService{},
	}

	resp, err := svc.ReconcileAdminSubscription(context.Background(), "sub_789")
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if resp.Subscription.Status != models.SubscriptionStatusActive {
		t.Fatalf("expected active status, got %s", resp.Subscription.Status)
	}
	if !resp.Subscription.CancelAtCycleEnd {
		t.Fatalf("expected cancel_at_cycle_end to be true")
	}
	if resp.Subscription.CurrentPeriodEnd.Before(now.AddDate(0, 0, 19)) {
		t.Fatalf("expected reconciled current period end to move forward")
	}
}

type fakePlanService struct{}

func (f *fakePlanService) CreatePlan(ctx context.Context, req *models.CreatePlanRequest) (*models.Plan, error) {
	return &models.Plan{}, nil
}

func (f *fakePlanService) GetActivePlans(ctx context.Context) ([]models.Plan, error) {
	return nil, nil
}

func (f *fakePlanService) GetPlanByID(ctx context.Context, planID string) (*models.Plan, error) {
	return &models.Plan{PlanID: planID, Name: "Pro", RazorpayPlanID: "rp_pro"}, nil
}

func (f *fakePlanService) GetAllPlans(ctx context.Context) ([]models.Plan, error) {
	return []models.Plan{{PlanID: "pro", Name: "Pro", RazorpayPlanID: "rp_pro"}}, nil
}

func (f *fakePlanService) UpdatePlan(ctx context.Context, planID string, req *models.UpdatePlanRequest) error {
	return nil
}

func (f *fakePlanService) DeletePlan(ctx context.Context, planID string) error {
	return nil
}

func TestWebhookSubscriptionCancelledMarksFinalState(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID:     "sub_789",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		Amount:             19900,
		Currency:           "USD",
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 10),
		CancelAtCycleEnd:   true,
		CancelScheduledAt:  &now,
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	})
	emailSvc := &fakeEmailService{}
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, emailSvc)

	payload := &models.WebhookPayload{
		Event: "subscription.cancelled",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{
					"id": "sub_789",
				},
			},
		},
	}

	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("handle webhook returned error: %v", err)
	}

	updated := repo.subs["sub_789"]
	if updated.Status != models.SubscriptionStatusCancelled {
		t.Fatalf("expected status cancelled, got %s", updated.Status)
	}
	if updated.CancelledAt == nil {
		t.Fatalf("expected CancelledAt to be set")
	}
	for i := 0; i < 20 && len(emailSvc.cancellationCalls) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if len(emailSvc.cancellationCalls) != 1 {
		t.Fatalf("expected one cancellation email call, got %d", len(emailSvc.cancellationCalls))
	}
	if emailSvc.cancellationCalls[0].accessUntil.IsZero() {
		t.Fatalf("expected access until date in cancellation email")
	}
}

func TestWebhookRawRejectsMissingSignature(t *testing.T) {
	svc := &subscriptionService{
		webhookSecret: "whsec_test",
	}

	err := svc.HandleWebhookRaw(context.Background(), []byte(`{"event":"subscription.cancelled","payload":{}}`), "")
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
	if !strings.Contains(err.Error(), "missing webhook signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRawRejectsInvalidSignature(t *testing.T) {
	svc := &subscriptionService{
		webhookSecret: "whsec_test",
	}

	err := svc.HandleWebhookRaw(context.Background(), []byte(`{"event":"subscription.cancelled","payload":{}}`), "bad-signature")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if !strings.Contains(err.Error(), "invalid webhook signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRawAcceptsValidSignature(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID:     "sub_raw_1",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		Amount:             19900,
		Currency:           "USD",
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 10),
		CancelAtCycleEnd:   true,
		CancelScheduledAt:  &now,
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	})
	emailSvc := &fakeEmailService{}
	svc := &subscriptionService{
		subRepo:        repo,
		razorpay:       &fakeRazorpayGateway{},
		planService:    &fakePlanService{},
		emailService:   emailSvc,
		webhookSecret:  "whsec_test",
		creditsService: &fakeCreditsService{},
	}

	rawPayload := []byte(`{"event":"subscription.cancelled","payload":{"subscription":{"entity":{"id":"sub_raw_1"}}}}`)
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	mac.Write(rawPayload)
	signature := hex.EncodeToString(mac.Sum(nil))

	if err := svc.HandleWebhookRaw(context.Background(), rawPayload, signature); err != nil {
		t.Fatalf("handle webhook raw returned error: %v", err)
	}

	updated := repo.subs["sub_raw_1"]
	if updated.Status != models.SubscriptionStatusCancelled {
		t.Fatalf("expected status cancelled, got %s", updated.Status)
	}
}

type fakeCreditsService struct{}

func (f *fakeCreditsService) AddCredits(ctx context.Context, req *models.AddCreditsRequest) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: req.UserID, Credits: req.Amount}, nil
}

func (f *fakeCreditsService) GetBalanceByEmail(ctx context.Context, email string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: email, Credits: 0}, nil
}

func (f *fakeCreditsService) DeductCredits(ctx context.Context, req *models.DeductCreditsRequest) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: req.UserID, Credits: 0}, nil
}

func (f *fakeCreditsService) GetBalance(ctx context.Context, userID string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: userID, Credits: 0}, nil
}
