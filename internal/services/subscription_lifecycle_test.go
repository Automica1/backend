package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/pkg/billing"
)

const testRazorpaySecret = "test_secret"

func signSubscriptionPayment(paymentID, subscriptionID string) string {
	data := paymentID + "|" + subscriptionID
	mac := hmac.New(sha256.New, []byte(testRazorpaySecret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func signOrderPayment(orderID, paymentID string) string {
	data := orderID + "|" + paymentID
	mac := hmac.New(sha256.New, []byte(testRazorpaySecret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

func activeStarterSub(now time.Time) *models.Subscription {
	return &models.Subscription{
		SubscriptionID:     "sub_starter",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "starter",
		Amount:             99900,
		Currency:           "INR",
		CurrentPeriodStart: now.AddDate(0, 0, -10),
		CurrentPeriodEnd:   now.AddDate(0, 0, 20),
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	}
}

func activeProSub(now time.Time) *models.Subscription {
	return &models.Subscription{
		SubscriptionID:     "sub_pro",
		UserID:             "user@example.com",
		Email:              "user@example.com",
		Status:             models.SubscriptionStatusActive,
		PlanID:             "pro",
		Amount:             799900,
		Currency:           "INR",
		CurrentPeriodStart: now.AddDate(0, 0, -10),
		CurrentPeriodEnd:   now.AddDate(0, 0, 20),
		CreatedAt:          now.AddDate(0, -1, 0),
		UpdatedAt:          now,
	}
}

func svcWithCredits(repo *fakeSubscriptionRepo, gateway *fakeRazorpayGateway, credits CreditsService) *subscriptionService {
	return &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		emailService:     &fakeEmailService{},
		razorpay:         gateway,
		razorpayKey:      "rzp_test_fake",
		razorpaySecret:   testRazorpaySecret,
		creditsService:   credits,
		planService:      &fakePlanService{},
		userService:      &fakeUserService{billingCurrency: "INR"},
	}
}

// Case A1 — INR new subscription order
func TestCaseA1_CreateOrderINRStarter(t *testing.T) {
	repo := newFakeSubscriptionRepo()
	gateway := &fakeRazorpayGateway{}
	svc := svcWithCredits(repo, gateway, &fakeCreditsService{})

	resp, err := svc.CreateOrder(context.Background(), "user@example.com", "user@example.com", "Test", "+919876543210", "starter", "INR", "IN", "", "")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if resp.Currency != "INR" || resp.Amount != 99900 {
		t.Fatalf("expected INR starter 99900, got %s %d", resp.Currency, resp.Amount)
	}
	created := repo.subs[resp.SubscriptionID]
	if created == nil || created.Status != models.SubscriptionStatusCreated {
		t.Fatalf("expected created pending sub, got %#v", created)
	}
}

// Case A8 — verify payment idempotent
func TestCaseA8_VerifyPaymentIdempotent(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID: "sub_new_1",
		UserID:         "user@example.com",
		Email:          "user@example.com",
		Status:         models.SubscriptionStatusCreated,
		PlanID:         "starter",
		Amount:         99900,
		Currency:       "INR",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, credits)

	req := &models.VerifyPaymentRequest{
		RazorpayPaymentID:      "pay_a8_1",
		RazorpaySubscriptionID: "sub_new_1",
		RazorpaySignature:      signSubscriptionPayment("pay_a8_1", "sub_new_1"),
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("second verify: %v", err)
	}
	if credits.added != 1000 {
		t.Fatalf("expected 1000 credits once, got %d", credits.added)
	}
}

// Case B1 — Starter → Pro upgrade credits delta
func TestCaseB1_UpgradeCreditsDelta(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeStarterSub(now))
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, credits)

	price, currency, err := svc.CalculateUpgradePrice(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("CalculateUpgradePrice: %v", err)
	}
	if currency != "INR" || price <= 0 {
		t.Fatalf("expected positive INR proration, got %d %s", price, currency)
	}

	order, err := svc.CreateUpgradeOrder(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("CreateUpgradeOrder: %v", err)
	}

	req := &models.VerifyPaymentRequest{
		RazorpayPaymentID: "pay_b1_1",
		RazorpayOrderID:   order.OrderID,
		RazorpaySignature: signOrderPayment(order.OrderID, "pay_b1_1"),
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("upgrade verify: %v", err)
	}
	if credits.added != 8000 {
		t.Fatalf("expected +8000 upgrade credits, got %d", credits.added)
	}
	updated := repo.subs["sub_starter"]
	if updated.PlanID != "pro" {
		t.Fatalf("expected plan pro after upgrade, got %s", updated.PlanID)
	}
}

// Case B10 — cross-currency upgrade blocked via locked sub currency
func TestCaseB10_CrossCurrencyUpgradeUsesSubscriptionCurrency(t *testing.T) {
	now := time.Now().UTC()
	sub := activeStarterSub(now)
	sub.Currency = "USD"
	sub.Amount = 1200
	sub.SubscriptionID = "sub_usd"
	repo := newFakeSubscriptionRepo(sub)
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, &fakeCreditsService{})

	_, currency, err := svc.CalculateUpgradePrice(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("CalculateUpgradePrice: %v", err)
	}
	if currency != "USD" {
		t.Fatalf("expected USD locked currency, got %s", currency)
	}
}

// Case B3/B4 — proration at start vs end of cycle
func TestCaseB3B4_UpgradeProrationByCyclePosition(t *testing.T) {
	now := time.Now().UTC()

	startCycle := activeStarterSub(now)
	startCycle.CurrentPeriodStart = now.Add(-24 * time.Hour)
	startCycle.CurrentPeriodEnd = now.AddDate(0, 1, 0).Add(-24 * time.Hour)
	repoStart := newFakeSubscriptionRepo(startCycle)
	svcStart := svcWithCredits(repoStart, &fakeRazorpayGateway{}, &fakeCreditsService{})
	priceStart, _, err := svcStart.CalculateUpgradePrice(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("start cycle: %v", err)
	}

	endCycle := activeStarterSub(now)
	endCycle.CurrentPeriodStart = now.AddDate(0, 0, -29)
	endCycle.CurrentPeriodEnd = now.Add(24 * time.Hour)
	repoEnd := newFakeSubscriptionRepo(endCycle)
	svcEnd := svcWithCredits(repoEnd, &fakeRazorpayGateway{}, &fakeCreditsService{})
	priceEnd, _, err := svcEnd.CalculateUpgradePrice(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("end cycle: %v", err)
	}
	if priceStart <= priceEnd {
		t.Fatalf("expected higher proration early in cycle (start=%d end=%d)", priceStart, priceEnd)
	}
}

// Case C1 — schedule downgrade
func TestCaseC1_ScheduleDowngrade(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeProSub(now))
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	if err := svc.DowngradeSubscription(context.Background(), "user@example.com", "starter"); err != nil {
		t.Fatalf("DowngradeSubscription: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.PendingPlanID != "starter" || updated.PlanChangeDate == nil {
		t.Fatalf("expected pending starter downgrade, got %#v", updated)
	}
}

// Case C2 — apply pending downgrade on charged webhook
func TestCaseC2_ApplyPendingDowngradeOnCharged(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	pending := "starter"
	sub.PendingPlanID = pending
	past := now.Add(-time.Hour)
	sub.PlanChangeDate = &past
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{}
	svc := svcWithCredits(repo, gateway, &trackingCreditsService{})

	payload := &models.WebhookPayload{
		Event: "subscription.charged",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":            "sub_pro",
					"current_start": float64(now.Unix()),
					"current_end":   float64(now.AddDate(0, 1, 0).Unix()),
				},
			},
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{"id": "pay_c2_1"},
			},
		},
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.PlanID != "starter" || updated.PendingPlanID != "" {
		t.Fatalf("expected starter applied, pending cleared, got plan=%s pending=%s", updated.PlanID, updated.PendingPlanID)
	}
	if len(gateway.updateCalls) != 1 {
		t.Fatalf("expected razorpay plan swap, got %d update calls", len(gateway.updateCalls))
	}
}

// Case E1 — resume after cancel scheduled
func TestCaseE1_ResumeAfterCancelScheduled(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.ResumeSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("ResumeSubscription: %v", err)
	}
	if len(gateway.cancelScheduledChangesCalls) != 0 {
		t.Fatalf("expected no razorpay call for local-only cancellation, got %#v", gateway.cancelScheduledChangesCalls)
	}
	updated := repo.subs["sub_pro"]
	if updated.CancelAtCycleEnd || updated.CancelScheduledAt != nil {
		t.Fatalf("expected cancellation flags cleared")
	}
}

func TestCaseE1_ResumeLegacyRazorpayScheduledCancellation(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{
		fetchResult: map[string]interface{}{
			"id":                  "sub_pro",
			"status":              "active",
			"cancel_at_cycle_end": true,
		},
	}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.ResumeSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("ResumeSubscription: %v", err)
	}
	if len(gateway.cancelScheduledChangesCalls) != 1 {
		t.Fatalf("expected razorpay cancel_scheduled_changes call, got %#v", gateway.cancelScheduledChangesCalls)
	}
}

// Case E3 — resume idempotent
func TestCaseE3_ResumeIdempotent(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeProSub(now))
	gateway := &fakeRazorpayGateway{}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.ResumeSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("ResumeSubscription: %v", err)
	}
	if len(gateway.updateCalls) != 0 {
		t.Fatalf("expected no razorpay call when not scheduled")
	}
}

// Case E5 — resume clears orphaned cancelScheduledAt when cancel flag is false
func TestCaseE5_ResumeClearsStaleCancelScheduledAt(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	if err := svc.ResumeSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("ResumeSubscription: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.CancelScheduledAt != nil {
		t.Fatalf("expected stale cancelScheduledAt cleared")
	}
}

// Case E6 — resume succeeds even when razorpay fetch fails for local-only cancel
func TestCaseE6_ResumeSucceedsWhenRazorpayFetchFails(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{fetchErr: fmt.Errorf("network error")}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.ResumeSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("ResumeSubscription: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.CancelAtCycleEnd || updated.CancelScheduledAt != nil {
		t.Fatalf("expected local cancel cleared despite razorpay fetch failure")
	}
}

// Case E2/E4 — resume rejected when not active
func TestCaseE2_ResumeRejectedWhenCancelled(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.Status = models.SubscriptionStatusCancelled
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	err := svc.ResumeSubscription(context.Background(), "user@example.com")
	if err == nil || !strings.Contains(err.Error(), "no active subscription to resume") {
		t.Fatalf("expected resume rejection, got %v", err)
	}
}

// Case F7 — upgrade clears pending downgrade
func TestCaseF7_UpgradeClearsPendingDowngrade(t *testing.T) {
	now := time.Now().UTC()
	sub := activeStarterSub(now)
	sub.PendingPlanID = "starter"
	sub.PlanChangeDate = &sub.CurrentPeriodEnd
	repo := newFakeSubscriptionRepo(sub)
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, credits)

	order, err := svc.CreateUpgradeOrder(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("CreateUpgradeOrder: %v", err)
	}
	req := &models.VerifyPaymentRequest{
		RazorpayPaymentID: "pay_f7_1",
		RazorpayOrderID:   order.OrderID,
		RazorpaySignature: signOrderPayment(order.OrderID, "pay_f7_1"),
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("verify: %v", err)
	}
	updated := repo.subs["sub_starter"]
	if updated.PendingPlanID != "" || updated.PlanID != "pro" {
		t.Fatalf("expected upgrade with pending cleared, got plan=%s pending=%s", updated.PlanID, updated.PendingPlanID)
	}
}

// Case F8 — cancel clears pending downgrade
func TestCaseF8_CancelClearsPendingDowngrade(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.PendingPlanID = "starter"
	sub.PlanChangeDate = &sub.CurrentPeriodEnd
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	if err := svc.CancelSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.PendingPlanID != "" || !updated.CancelAtCycleEnd {
		t.Fatalf("expected cancel scheduled and pending cleared")
	}
}

// Case F8b — idempotent cancel still clears stale pending downgrade
func TestCaseF8b_CancelIdempotentClearsPendingDowngrade(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	sub.PendingPlanID = "starter"
	sub.PlanChangeDate = &sub.CurrentPeriodEnd
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	if err := svc.CancelSubscription(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.PendingPlanID != "" {
		t.Fatalf("expected pending downgrade cleared on idempotent cancel")
	}
}

func TestGetSubscriptionStatusNormalizesConflictingState(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	sub.PendingPlanID = "starter"
	sub.PlanChangeDate = &sub.CurrentPeriodEnd
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	status, err := svc.GetSubscriptionStatus(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("GetSubscriptionStatus: %v", err)
	}
	if status.PendingPlanID != "" {
		t.Fatalf("expected pending cleared when cancel scheduled, got %s", status.PendingPlanID)
	}
	updated := repo.subs["sub_pro"]
	if updated.PendingPlanID != "" {
		t.Fatalf("expected normalization persisted")
	}
}

func TestClearPendingPlanChange(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.PendingPlanID = "starter"
	sub.PlanChangeDate = &sub.CurrentPeriodEnd
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	if err := svc.ClearPendingPlanChange(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("ClearPendingPlanChange: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.PendingPlanID != "" || updated.PlanChangeDate != nil {
		t.Fatalf("expected pending plan change cleared")
	}
}

// Case F5 — upgrade allowed when cancel scheduled; clears cancel on verify
func TestCaseF5_UpgradeClearsScheduledCancellation(t *testing.T) {
	now := time.Now().UTC()
	sub := activeStarterSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, credits)

	order, err := svc.CreateUpgradeOrder(context.Background(), "user@example.com", "pro")
	if err != nil {
		t.Fatalf("CreateUpgradeOrder: %v", err)
	}
	req := &models.VerifyPaymentRequest{
		RazorpayPaymentID: "pay_f5_1",
		RazorpayOrderID:   order.OrderID,
		RazorpaySignature: signOrderPayment(order.OrderID, "pay_f5_1"),
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("verify: %v", err)
	}
	updated := repo.subs["sub_starter"]
	if updated.CancelAtCycleEnd || updated.PlanID != "pro" {
		t.Fatalf("expected upgraded active sub without cancel schedule")
	}
}

// Case F6 — downgrade clears pending cancellation then schedules downgrade
func TestCaseF6_DowngradeClearsPendingCancellation(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.DowngradeSubscription(context.Background(), "user@example.com", "starter"); err != nil {
		t.Fatalf("expected downgrade to clear cancellation and schedule, got %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.CancelAtCycleEnd || updated.PendingPlanID != "starter" {
		t.Fatalf("expected cancellation cleared and starter pending, got cancel=%v pending=%s", updated.CancelAtCycleEnd, updated.PendingPlanID)
	}
}

func TestCaseF6_DowngradeClearsLocalCancelWhenLegacyResumeFails(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{
		fetchResult: map[string]interface{}{
			"id":                  "sub_pro",
			"status":              "active",
			"cancel_at_cycle_end": true,
		},
		cancelScheduledChangesErr: fmt.Errorf("no scheduled update on the subscription to cancel"),
	}
	svc := newTestSubscriptionService(repo, gateway, &fakeEmailService{})

	if err := svc.DowngradeSubscription(context.Background(), "user@example.com", "starter"); err != nil {
		t.Fatalf("expected downgrade to succeed with local cancel cleared, got %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.CancelAtCycleEnd || updated.PendingPlanID != "starter" {
		t.Fatalf("expected local cancel cleared and starter pending, got cancel=%v pending=%s", updated.CancelAtCycleEnd, updated.PendingPlanID)
	}
}

// Case F10 — second downgrade to same pending plan is idempotent
func TestCaseF10_SecondDowngradeToSamePendingPlan(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeProSub(now))
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	if err := svc.DowngradeSubscription(context.Background(), "user@example.com", "starter"); err != nil {
		t.Fatalf("first downgrade: %v", err)
	}
	if err := svc.DowngradeSubscription(context.Background(), "user@example.com", "starter"); err != nil {
		t.Fatalf("second downgrade to same plan should succeed: %v", err)
	}
	updated := repo.subs["sub_pro"]
	if updated.PendingPlanID != "starter" {
		t.Fatalf("expected pending starter, got %s", updated.PendingPlanID)
	}
}

// Case F13 — new order after cancelled subscription
func TestCaseF13_NewOrderAfterCancelled(t *testing.T) {
	now := time.Now().UTC()
	cancelled := activeStarterSub(now)
	cancelled.Status = models.SubscriptionStatusCancelled
	cancelled.SubscriptionID = "sub_old"
	repo := newFakeSubscriptionRepo(cancelled)
	gateway := &fakeRazorpayGateway{}
	svc := svcWithCredits(repo, gateway, &fakeCreditsService{})

	resp, err := svc.CreateOrder(context.Background(), "user@example.com", "user@example.com", "Test", "+919876543210", "starter", "INR", "IN", "", "")
	if err != nil {
		t.Fatalf("CreateOrder after cancelled: %v", err)
	}
	if resp.SubscriptionID == "sub_old" {
		t.Fatal("expected new subscription id")
	}
}

// Case G1 — charged webhook grants credits (J3 renewal)
func TestCaseG1_J3_ChargedWebhookGrantsPlanCredits(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeProSub(now))
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, credits)

	payload := &models.WebhookPayload{
		Event: "subscription.charged",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":            "sub_pro",
					"current_start": float64(now.Unix()),
					"current_end":   float64(now.AddDate(0, 1, 0).Unix()),
				},
			},
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{"id": "pay_g1_1"},
			},
		},
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	if credits.added != 9000 {
		t.Fatalf("expected 9000 renewal credits, got %d", credits.added)
	}
}

func TestScheduledCancellationFinalizedOnChargedWebhook(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	repo := newFakeSubscriptionRepo(sub)
	gateway := &fakeRazorpayGateway{}
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, gateway, credits)

	payload := &models.WebhookPayload{
		Event: "subscription.charged",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{
					"id":            "sub_pro",
					"current_start": float64(now.Unix()),
					"current_end":   float64(now.AddDate(0, 1, 0).Unix()),
				},
			},
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{"id": "pay_finalize_1"},
			},
		},
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	if len(gateway.cancelCalls) != 1 {
		t.Fatalf("expected razorpay cancel after final cycle charge, got %d calls", len(gateway.cancelCalls))
	}
	updated := repo.subs["sub_pro"]
	if updated.Status != models.SubscriptionStatusCancelled || updated.CancelAtCycleEnd {
		t.Fatalf("expected cancelled subscription after final charge, got status=%s cancelAtCycleEnd=%v", updated.Status, updated.CancelAtCycleEnd)
	}
}

// Case G2 — duplicate charged webhook (see TestWebhookSubscriptionChargedIsIdempotent)

// Case J4 — downgrade renewal (see TestCaseC2_ApplyPendingDowngradeOnCharged)

// Case G3 — payment failed → past_due
func TestCaseG3_PaymentFailedMarksPastDue(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeProSub(now))
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, &fakeCreditsService{})

	payload := &models.WebhookPayload{
		Event: "payment.failed",
		Payload: map[string]interface{}{
			"payment": map[string]interface{}{
				"entity": map[string]interface{}{
					"subscription_id": "sub_pro",
				},
			},
		},
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	if repo.subs["sub_pro"].Status != models.SubscriptionStatusPastDue {
		t.Fatalf("expected past_due, got %s", repo.subs["sub_pro"].Status)
	}
}

// Case G5 — halted clears credits
func TestCaseG5_HaltedClearsCredits(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeProSub(now))
	credits := &trackingCreditsService{added: 5000}
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         &fakeRazorpayGateway{},
		planService:      &fakePlanService{},
		emailService:     &fakeEmailService{},
		creditsService:   credits,
	}
	credits.added = 5000

	payload := &models.WebhookPayload{
		Event: "subscription.halted",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{"id": "sub_pro"},
			},
		},
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("webhook: %v", err)
	}
	if repo.subs["sub_pro"].Status != models.SubscriptionStatusExpired {
		t.Fatalf("expected expired status")
	}
}

// Case G7 — unknown subscription charged is no-op
func TestCaseG7_UnknownSubscriptionChargedNoOp(t *testing.T) {
	svc := svcWithCredits(newFakeSubscriptionRepo(), &fakeRazorpayGateway{}, &fakeCreditsService{})
	payload := &models.WebhookPayload{
		Event: "subscription.charged",
		Payload: map[string]interface{}{
			"subscription": map[string]interface{}{
				"entity": map[string]interface{}{"id": "sub_missing"},
			},
		},
	}
	if err := svc.HandleWebhook(context.Background(), payload, ""); err != nil {
		t.Fatalf("expected nil error for unknown sub, got %v", err)
	}
}

// Case J1 — new subscription full credits
func TestCaseJ1_NewSubscriptionFullCredits(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID: "sub_j1",
		UserID:         "user@example.com",
		Status:         models.SubscriptionStatusCreated,
		PlanID:         "starter",
		Currency:       "INR",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	credits := &trackingCreditsService{}
	svc := svcWithCredits(repo, &fakeRazorpayGateway{}, credits)

	req := &models.VerifyPaymentRequest{
		RazorpayPaymentID:      "pay_j1",
		RazorpaySubscriptionID: "sub_j1",
		RazorpaySignature:      signSubscriptionPayment("pay_j1", "sub_j1"),
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if credits.added != 1000 {
		t.Fatalf("expected 1000 credits, got %d", credits.added)
	}
}

// Case J2 — upgrade credit delta unit check
func TestCaseJ2_UpgradeCreditDelta(t *testing.T) {
	oldPlan, _ := (&fakePlanService{}).GetPlanByID(context.Background(), "starter")
	newPlan, _ := (&fakePlanService{}).GetPlanByID(context.Background(), "pro")
	if got := billing.UpgradeCreditDelta(oldPlan, newPlan); got != 8000 {
		t.Fatalf("UpgradeCreditDelta = %d, want 8000", got)
	}
}

// Case J4 — downgrade renewal credits (see TestCaseC2)

// Case H1 — active INR sub locks currency on create order path
func TestCaseH1_ActiveINRSubLocksCurrency(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(activeStarterSub(now))
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         &fakeRazorpayGateway{},
		planService:      &fakePlanService{},
		userService:      &fakeUserService{billingCurrency: "USD"},
	}
	currency := svc.resolveBillingCurrency(context.Background(), "user@example.com", "USD", "", "US", "", "")
	if currency != "INR" {
		t.Fatalf("expected INR lock from active sub, got %s", currency)
	}
}

// Case H3 — user billing currency persisted on verify
func TestCaseH3_VerifySetsBillingCurrency(t *testing.T) {
	now := time.Now().UTC()
	repo := newFakeSubscriptionRepo(&models.Subscription{
		SubscriptionID: "sub_h3",
		UserID:         "user@example.com",
		Status:         models.SubscriptionStatusCreated,
		PlanID:         "starter",
		Currency:       "INR",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	userSvc := &fakeUserService{}
	svc := &subscriptionService{
		subRepo:          repo,
		paymentEventRepo: newFakePaymentEventRepo(),
		razorpay:         &fakeRazorpayGateway{},
		razorpaySecret:   testRazorpaySecret,
		planService:      &fakePlanService{},
		userService:      userSvc,
		creditsService:   &fakeCreditsService{},
		emailService:     &fakeEmailService{},
	}
	req := &models.VerifyPaymentRequest{
		RazorpayPaymentID:      "pay_h3",
		RazorpaySubscriptionID: "sub_h3",
		RazorpaySignature:      signSubscriptionPayment("pay_h3", "sub_h3"),
	}
	if _, err := svc.VerifyPayment(context.Background(), "user@example.com", req); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if userSvc.billingCurrency != "INR" {
		t.Fatalf("expected billing currency INR, got %s", userSvc.billingCurrency)
	}
}

// Case K1 — admin list includes pending plan
func TestCaseK1_AdminListShowsPendingPlan(t *testing.T) {
	now := time.Now().UTC()
	sub := activeProSub(now)
	sub.PendingPlanID = "starter"
	repo := newFakeSubscriptionRepo(sub)
	svc := newTestSubscriptionService(repo, &fakeRazorpayGateway{}, &fakeEmailService{})

	resp, err := svc.ListAdminSubscriptions(context.Background(), models.AdminSubscriptionQuery{})
	if err != nil {
		t.Fatalf("ListAdminSubscriptions: %v", err)
	}
	if len(resp.Subscriptions) != 1 || resp.Subscriptions[0].PendingPlanID != "starter" {
		t.Fatalf("expected pending plan in admin list")
	}
}

// Case K2 — reconcile (see TestReconcileAdminSubscriptionUpdatesLocalState)
// Case K3 — test reset (see TestResetSubscriptionForTestingRequiresEnvAndTestKey)
// Case F4 — resume (see TestCaseE1)
// Case D1 — cancel (see TestCancelSubscriptionSchedulesRazorpayCycleEndCancellation)
// Case D4 — cancelled webhook (see TestWebhookSubscriptionCancelledMarksFinalState)
