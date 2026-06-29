package services

import (
	"context"
	"fmt"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/pkg/billing"
)

const (
	paymentEventVerify  = "verify_payment"
	paymentEventWebhook = "webhook_charged"
)

func (s *subscriptionService) swapRazorpayPlan(ctx context.Context, subscriptionID, planID, currency string) error {
	newPlan, err := s.planService.GetPlanByID(ctx, planID)
	if err != nil {
		return err
	}
	_, razorpayPlanID, ok := billing.ResolvePlanPricing(newPlan, currency)
	if !ok {
		return fmt.Errorf("plan %s is not configured for %s", planID, currency)
	}

	_, err = s.razorpay.UpdateSubscription(subscriptionID, map[string]interface{}{
		"plan_id":            razorpayPlanID,
		"schedule_change_at": "now",
		"customer_notify":    1,
	})
	return err
}

func (s *subscriptionService) applyPendingPlanChange(ctx context.Context, sub *models.Subscription) error {
	if sub == nil || sub.PendingPlanID == "" || sub.PlanChangeDate == nil {
		return nil
	}
	if time.Now().Before(*sub.PlanChangeDate) {
		return nil
	}

	newPlan, err := s.planService.GetPlanByID(ctx, sub.PendingPlanID)
	if err != nil {
		return err
	}

	currency := sub.Currency
	if currency == "" {
		currency = billing.CurrencyUSD
	}

	if err := s.swapRazorpayPlan(ctx, sub.SubscriptionID, newPlan.PlanID, currency); err != nil {
		fmt.Printf("[applyPendingPlanChange] Razorpay plan swap failed for %s: %v\n", sub.SubscriptionID, err)
	}

	sub.PlanID = newPlan.PlanID
	sub.Amount = billing.PlanAmountInCurrency(newPlan, currency)
	sub.PendingPlanID = ""
	sub.PlanChangeDate = nil
	sub.UpdatedAt = time.Now()
	return s.subRepo.Update(ctx, sub)
}

func (s *subscriptionService) syncSubscriptionPeriod(sub *models.Subscription, entity map[string]interface{}) {
	if currentStart, ok := parseRemoteTime(entity["current_start"]); ok {
		sub.CurrentPeriodStart = currentStart
	}
	if currentEnd, ok := parseRemoteTime(entity["current_end"]); ok {
		sub.CurrentPeriodEnd = currentEnd
	}
}

func (s *subscriptionService) extractWebhookPaymentID(payload *models.WebhookPayload) string {
	if payload == nil {
		return ""
	}
	paymentData, ok := payload.Payload["payment"].(map[string]interface{})
	if !ok {
		return ""
	}
	entity, ok := paymentData["entity"].(map[string]interface{})
	if !ok {
		return ""
	}
	if paymentID, ok := entity["id"].(string); ok {
		return paymentID
	}
	return ""
}

func (s *subscriptionService) grantCreditsOnce(ctx context.Context, paymentID, eventType, userID, subscriptionID string, credits int) (int, error) {
	if credits <= 0 {
		return 0, nil
	}
	if paymentID == "" {
		paymentID = fmt.Sprintf("%s:%s:%d", eventType, subscriptionID, time.Now().Unix())
	}

	alreadyProcessed, err := s.paymentEventRepo.TryRecord(ctx, paymentID, eventType, userID, subscriptionID, credits)
	if err != nil {
		return 0, err
	}
	if alreadyProcessed {
		return 0, nil
	}

	_, err = s.creditsService.AddCredits(ctx, &models.AddCreditsRequest{
		UserID: userID,
		Amount: credits,
	})
	if err != nil {
		return 0, err
	}
	return credits, nil
}

func (s *subscriptionService) buildVerifyIdempotentResponse(ctx context.Context, userID string, sub *models.Subscription) (*models.SubscriptionResponse, error) {
	user, err := s.userService.GetOrCreateUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	balance, err := s.creditsService.GetBalance(ctx, user.UserID)
	if err != nil {
		return nil, err
	}
	return &models.SubscriptionResponse{
		Message:          "Payment already processed",
		Status:           sub.Status,
		CreditsAdded:     0,
		RemainingCredits: balance.Credits,
	}, nil
}
