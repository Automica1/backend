package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"
)

type fakeRazorpayGateway struct {
	cancelCalls []cancelCall
	cancelErr   error
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
