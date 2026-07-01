package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

type fakeRazorpayGateway struct {
	cancelCalls []cancelCall
	updateCalls []updateCall
	cancelErr   error
	updateErr   error
	fetchResult map[string]interface{}
	fetchErr    error
	subCounter  int
}

type updateCall struct {
	subscriptionID string
	data           map[string]interface{}
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
	f.subCounter++
	return map[string]interface{}{
		"id":        fmt.Sprintf("sub_test_%d", f.subCounter),
		"short_url": "https://rzp.io/test",
	}, nil
}

func (f *fakeRazorpayGateway) UpdateSubscription(subscriptionID string, data map[string]interface{}) (map[string]interface{}, error) {
	callData := map[string]interface{}{}
	for k, v := range data {
		callData[k] = v
	}
	f.updateCalls = append(f.updateCalls, updateCall{
		subscriptionID: subscriptionID,
		data:           callData,
	})
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return map[string]interface{}{"id": subscriptionID}, nil
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
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "subscription not found by status", "")
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

func (r *fakeSubscriptionRepo) ListAllByUserID(ctx context.Context, userID string) ([]models.Subscription, error) {
	result := make([]models.Subscription, 0)
	for _, sub := range r.subs {
		if sub.UserID == userID {
			result = append(result, *sub)
		}
	}
	return result, nil
}

func (r *fakeSubscriptionRepo) DeleteByUserID(ctx context.Context, userID string) (int64, error) {
	var deleted int64
	for id, sub := range r.subs {
		if sub.UserID == userID {
			delete(r.subs, id)
			deleted++
		}
	}
	return deleted, nil
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
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		emailService:     emailSvc,
		razorpay:         gateway,
		razorpayKey:      "rzp_test_fake",
		razorpaySecret:   "test_secret",
		creditsService:   &fakeCreditsService{},
		planService:      &fakePlanService{},
		userService:      &fakeUserService{billingCurrency: "INR"},
	}
}

type fakePaymentEventRepo struct {
	processed map[string]struct{}
}

func newFakePaymentEventRepo() *fakePaymentEventRepo {
	return &fakePaymentEventRepo{processed: map[string]struct{}{}}
}

func (f *fakePaymentEventRepo) TryRecord(ctx context.Context, paymentID, eventType, userID, subscriptionID string, creditsAdded int) (bool, error) {
	if _, ok := f.processed[paymentID]; ok {
		return true, nil
	}
	f.processed[paymentID] = struct{}{}
	return false, nil
}

func (f *fakePaymentEventRepo) IsProcessed(ctx context.Context, paymentID string) (bool, error) {
	_, ok := f.processed[paymentID]
	return ok, nil
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
	if got := gateway.cancelCalls[0].data["cancel_at_cycle_end"]; got != true {
		t.Fatalf("expected cancel_at_cycle_end=true, got %#v", got)
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
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         gateway,
		planService:      &fakePlanService{},
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

func (f *fakePlanService) GetActivePlans(ctx context.Context, currency string) ([]models.PublicPlan, error) {
	return nil, nil
}

func (f *fakePlanService) GetPlanByID(ctx context.Context, planID string) (*models.Plan, error) {
	if planID == "starter" {
		return &models.Plan{
			PlanID:         planID,
			Name:           "Starter",
			IsActive:       true,
			Credits:        1000,
			RazorpayPlanID: "rp_starter_usd",
			Price:          1200,
			Pricing: map[string]models.PlanCurrencyPricing{
				"USD": {Amount: 1200, RazorpayPlanID: "rp_starter_usd"},
				"INR": {Amount: 99900, RazorpayPlanID: "rp_starter_inr"},
			},
		}, nil
	}
	return &models.Plan{
		PlanID:         planID,
		Name:           "Pro",
		IsActive:       true,
		Credits:        9000,
		RazorpayPlanID: "rp_pro_usd",
		Price:          9900,
		Pricing: map[string]models.PlanCurrencyPricing{
			"USD": {Amount: 9900, RazorpayPlanID: "rp_pro_usd"},
			"INR": {Amount: 799900, RazorpayPlanID: "rp_pro_inr"},
		},
	}, nil
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
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         &fakeRazorpayGateway{},
		planService:      &fakePlanService{},
		emailService:     emailSvc,
		webhookSecret:    "whsec_test",
		creditsService:   &fakeCreditsService{},
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

type fakeUserService struct {
	billingCurrency string
}

func (f *fakeUserService) RegisterUser(ctx context.Context, req *models.RegisterUserRequest) (*models.RegisterUserResponse, error) {
	return nil, nil
}

func (f *fakeUserService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return &models.User{UserID: email, Email: email, BillingCurrency: f.billingCurrency}, nil
}

func (f *fakeUserService) GetOrCreateUser(ctx context.Context, email string) (*models.User, error) {
	return &models.User{UserID: email, Email: email, BillingCurrency: f.billingCurrency}, nil
}

func (f *fakeUserService) GetAllUsers(ctx context.Context) (*models.AdminUserListResponse, error) {
	return nil, nil
}

func (f *fakeUserService) ListUsers(ctx context.Context, query models.AdminListQuery) (*models.AdminUserListResponse, error) {
	return nil, nil
}

func (f *fakeUserService) GetUserByID(ctx context.Context, userID string) (*models.AdminUserDetailResponse, error) {
	return nil, nil
}

func (f *fakeUserService) GetUserStats(ctx context.Context) (*models.UserStatsResponse, error) {
	return nil, nil
}

func (f *fakeUserService) GetUserActivity(ctx context.Context, userID string) (*models.UserActivityResponse, error) {
	return nil, nil
}

func (f *fakeUserService) GetUserCredits(ctx context.Context, userID string) (*models.UserCreditsResponse, error) {
	return nil, nil
}

func (f *fakeUserService) DeleteUser(ctx context.Context, userID string) error {
	return nil
}

func (f *fakeUserService) SuspendUser(ctx context.Context, userID string) error {
	return nil
}

func (f *fakeUserService) ReactivateUser(ctx context.Context, userID string) error {
	return nil
}

func (f *fakeUserService) SetBillingCurrency(ctx context.Context, userID, currency string) error {
	f.billingCurrency = currency
	return nil
}

func (f *fakeUserService) ClearBillingCurrency(ctx context.Context, userID string) error {
	f.billingCurrency = ""
	return nil
}

func TestCreateOrderUsesINRForIndianPhone(t *testing.T) {
	repo := newFakeSubscriptionRepo()
	gateway := &fakeRazorpayGateway{}
	planSvc := &fakePlanService{}
	userSvc := &fakeUserService{}
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         gateway,
		planService:      planSvc,
		userService:      userSvc,
	}

	resp, err := svc.CreateOrder(context.Background(), "user@example.com", "user@example.com", "Test User", "+919876543210", "pro", "", "", "", "")
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if resp.Currency != "INR" {
		t.Fatalf("expected INR currency, got %s", resp.Currency)
	}
	if resp.Amount != 799900 {
		t.Fatalf("expected INR amount 799900, got %d", resp.Amount)
	}

	created := repo.subs[resp.SubscriptionID]
	if created == nil || created.Currency != "INR" {
		t.Fatalf("expected created subscription currency INR, got %#v", created)
	}
}

func TestCreateOrderDefaultsToUSD(t *testing.T) {
	repo := newFakeSubscriptionRepo()
	gateway := &fakeRazorpayGateway{}
	planSvc := &fakePlanService{}
	userSvc := &fakeUserService{}
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         gateway,
		planService:      planSvc,
		userService:      userSvc,
	}

	resp, err := svc.CreateOrder(context.Background(), "user@example.com", "user@example.com", "Test User", "", "pro", "", "", "", "")
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if resp.Currency != "USD" {
		t.Fatalf("expected USD currency, got %s", resp.Currency)
	}
	if resp.Amount != 9900 {
		t.Fatalf("expected USD amount 9900, got %d", resp.Amount)
	}
}

func TestCreateOrderUsesINRForCountryHeader(t *testing.T) {
	repo := newFakeSubscriptionRepo()
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         &fakeRazorpayGateway{},
		planService:      &fakePlanService{},
		userService:      &fakeUserService{},
	}

	resp, err := svc.CreateOrder(context.Background(), "user@example.com", "user@example.com", "Test User", "", "pro", "", "IN", "", "")
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if resp.Currency != "INR" {
		t.Fatalf("expected INR currency, got %s", resp.Currency)
	}
}

type trackingCreditsService struct {
	added int
}

func (f *trackingCreditsService) AddCredits(ctx context.Context, req *models.AddCreditsRequest) (*models.CreditsResponse, error) {
	f.added += req.Amount
	return &models.CreditsResponse{UserID: req.UserID, Credits: f.added}, nil
}

func (f *trackingCreditsService) GetBalanceByEmail(ctx context.Context, email string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: email, Credits: f.added}, nil
}

func (f *trackingCreditsService) DeductCredits(ctx context.Context, req *models.DeductCreditsRequest) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: req.UserID, Credits: 0}, nil
}

func (f *trackingCreditsService) GetBalance(ctx context.Context, userID string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: userID, Credits: f.added}, nil
}

func TestWebhookSubscriptionChargedIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID:     "sub_charge_1",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		Amount:             9900,
		Currency:           "USD",
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   now.AddDate(0, 0, 10),
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	})
	credits := &trackingCreditsService{}
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         &fakeRazorpayGateway{},
		planService:      &fakePlanService{},
		emailService:     &fakeEmailService{},
		creditsService:   credits,
	}

	payload := &models.WebhookPayload{
		Event: "subscription.charged",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":            "sub_charge_1",
					"current_start": float64(now.Unix()),
					"current_end":   float64(now.AddDate(0, 1, 0).Unix()),
				},
			},
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"id": "pay_same_1",
				},
			},
		},
	}

	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("first webhook failed: %v", err)
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("second webhook failed: %v", err)
	}
	if credits.added != 9000 {
		t.Fatalf("expected credits added once (9000), got %d", credits.added)
	}
}

func TestResetSubscriptionForTestingRequiresEnvAndTestKey(t *testing.T) {
	t.Setenv("ALLOW_SUBSCRIPTION_TEST_RESET", "1")
	t.Cleanup(func() {
		_ = os.Unsetenv("ALLOW_SUBSCRIPTION_TEST_RESET")
	})

	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID: "sub_reset_1",
		UserID:         "user@example.com",
		Email:          "user@example.com",
		Status:         models.SubscriptionStatusActive,
	})
	gateway := &fakeRazorpayGateway{}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	_, err := svc.ResetSubscriptionForTesting(context.Background(), "sub_reset_1", "RESET")
	if err != nil {
		t.Fatalf("expected reset to succeed, got %v", err)
	}
	if len(gateway.cancelCalls) != 1 {
		t.Fatalf("expected one immediate cancel call, got %d", len(gateway.cancelCalls))
	}
	if got := gateway.cancelCalls[0].data["cancel_at_cycle_end"]; got != false {
		t.Fatalf("expected immediate cancel, got %#v", got)
	}
	if len(repo.subs) != 0 {
		t.Fatalf("expected local subscription records to be deleted")
	}
}

func TestResetSubscriptionForTestingBlockedWithoutOptIn(t *testing.T) {
	_ = os.Unsetenv("ALLOW_SUBSCRIPTION_TEST_RESET")
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID: "sub_reset_2",
		UserID:         "user@example.com",
		Status:         models.SubscriptionStatusActive,
	})
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	_, err := svc.ResetSubscriptionForTesting(context.Background(), "sub_reset_2", "RESET")
	if err == nil {
		t.Fatal("expected reset to be forbidden without env opt-in")
	}
}
