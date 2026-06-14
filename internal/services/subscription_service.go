// internal/services/subscription_service.go
package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"github.com/razorpay/razorpay-go"
)

type RazorpayGateway interface {
	CreateCustomer(data map[string]interface{}) (map[string]interface{}, error)
	CreateOrder(data map[string]interface{}) (map[string]interface{}, error)
	CreateSubscription(data map[string]interface{}) (map[string]interface{}, error)
	CancelSubscription(subscriptionID string, data map[string]interface{}) (map[string]interface{}, error)
	FetchSubscription(subscriptionID string) (map[string]interface{}, error)
}

type razorpayGateway struct {
	client *razorpay.Client
	key    string
	secret string
}

func newRazorpayGateway(key, secret string) RazorpayGateway {
	return &razorpayGateway{
		client: razorpay.NewClient(key, secret),
		key:    key,
		secret: secret,
	}
}

func (g *razorpayGateway) CreateCustomer(data map[string]interface{}) (map[string]interface{}, error) {
	return g.client.Customer.Create(data, nil)
}

func (g *razorpayGateway) CreateOrder(data map[string]interface{}) (map[string]interface{}, error) {
	return g.client.Order.Create(data, nil)
}

func (g *razorpayGateway) CreateSubscription(data map[string]interface{}) (map[string]interface{}, error) {
	return g.client.Subscription.Create(data, nil)
}

func (g *razorpayGateway) CancelSubscription(subscriptionID string, data map[string]interface{}) (map[string]interface{}, error) {
	return g.client.Subscription.Cancel(subscriptionID, data, nil)
}

func (g *razorpayGateway) FetchSubscription(subscriptionID string) (map[string]interface{}, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.razorpay.com/v1/subscriptions/%s", subscriptionID), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(g.key, g.secret)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("razorpay fetch failed: %s", strings.TrimSpace(string(body)))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

type SubscriptionService interface {
	CreateOrder(ctx context.Context, userID, email, name, contact, planID string) (*models.SubscriptionResponse, error)
	VerifyPayment(ctx context.Context, userID string, req *models.VerifyPaymentRequest) (*models.SubscriptionResponse, error)
	GetSubscriptionStatus(ctx context.Context, userID string) (*models.Subscription, error)
	CreateUpgradeOrder(ctx context.Context, userID, newPlanID string) (*models.SubscriptionResponse, error)
	CalculateUpgradePrice(ctx context.Context, userID, newPlanID string) (int, error)
	DowngradeSubscription(ctx context.Context, userID, newPlanID string) error
	CancelSubscription(ctx context.Context, userID string) error
	GetAllSubscriptions(ctx context.Context) ([]models.Subscription, error)
	GetActiveSubscriptionCount(ctx context.Context) (int64, error)
	ListAdminSubscriptions(ctx context.Context, query models.AdminSubscriptionQuery) (*models.AdminSubscriptionListResponse, error)
	GetAdminSubscription(ctx context.Context, subscriptionID string) (*models.AdminSubscriptionDetailResponse, error)
	ReconcileAdminSubscription(ctx context.Context, subscriptionID string) (*models.AdminSubscriptionDetailResponse, error)
	HandleWebhookRaw(ctx context.Context, rawPayload []byte, signature string) error
	HandleWebhook(ctx context.Context, payload *models.WebhookPayload, signature string) error
}

type subscriptionService struct {
	subRepo        repository.SubscriptionRepository
	creditsService CreditsService
	userService    UserService
	planService    PlanService
	emailService   EmailService
	razorpayKey    string
	razorpaySecret string
	webhookSecret  string
	razorpay       RazorpayGateway
}

func NewSubscriptionService(subRepo repository.SubscriptionRepository, creditsService CreditsService, userService UserService, planService PlanService, emailService EmailService, key, secret, webhookSecret string) SubscriptionService {
	return &subscriptionService{
		subRepo:        subRepo,
		creditsService: creditsService,
		userService:    userService,
		planService:    planService,
		emailService:   emailService,
		razorpayKey:    key,
		razorpaySecret: secret,
		webhookSecret:  webhookSecret,
		razorpay:       newRazorpayGateway(key, secret),
	}
}

func (s *subscriptionService) CreateOrder(ctx context.Context, userID, email, name, contact, planID string) (*models.SubscriptionResponse, error) {
	// Fetch plan details from DB
	plan, err := s.planService.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}

	if !plan.IsActive {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "plan is not active", "")
	}

	if plan.RazorpayPlanID == "" {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "plan is not configured for automated billing (missing Razorpay Plan ID)", "")
	}

	params := map[string]interface{}{
		"plan_id":         plan.RazorpayPlanID,
		"total_count":     120, // 10 years of monthly cycles
		"quantity":        1,
		"customer_notify": 1,
	}

	// Build customer params using provided fields when available
	customerParams := map[string]interface{}{}
	if name != "" {
		customerParams["name"] = name
	}
	if email != "" {
		customerParams["email"] = email
	}
	if contact != "" {
		customerParams["contact"] = contact
	}

	// If at least one customer field provided, attempt to create a Razorpay customer
	if len(customerParams) > 0 {
		custBody, custErr := s.razorpay.CreateCustomer(customerParams)
		if custErr == nil {
			if cid, ok := custBody["id"].(string); ok && cid != "" {
				params["customer_id"] = cid
				// store customer_id in notes — optional
				notes := map[string]string{"customer_id": cid}
				params["notes"] = notes
			}
		} else {
			fmt.Printf("[CreateOrder] Razorpay customer create error (ignored): %v\n", custErr)
		}
	} else {
		// Fallback: create minimal customer by email to help Razorpay link
		if email != "" {
			custBody, custErr := s.razorpay.CreateCustomer(map[string]interface{}{"email": email})
			if custErr == nil {
				if cid, ok := custBody["id"].(string); ok && cid != "" {
					params["customer_id"] = cid
					params["notes"] = map[string]string{"customer_id": cid}
				}
			} else {
				fmt.Printf("[CreateOrder] Razorpay customer create fallback error (ignored): %v\n", custErr)
			}
		}
	}

	fmt.Printf("[CreateOrder] Creating Razorpay Subscription for plan %s (RazorpayID: %s)\n", planID, plan.RazorpayPlanID)

	body, err := s.razorpay.CreateSubscription(params)
	if err != nil {
		fmt.Printf("[CreateOrder] Razorpay Error: %v\n", err)
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create razorpay subscription", err.Error())
	}

	subID := body["id"].(string)
	fmt.Printf("[CreateOrder] Created Subscription: %s\n", subID)

	// Store a pending subscription record
	sub := &models.Subscription{
		SubscriptionID: subID,
		UserID:         userID,
		Email:          email,
		Status:         models.SubscriptionStatusCreated,
		PlanID:         plan.PlanID,
		Amount:         plan.Price,
		Currency:       "USD",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.subRepo.Create(ctx, sub); err != nil {
		return nil, err
	}

	return &models.SubscriptionResponse{
		Message:        "Subscription order created successfully",
		SubscriptionID: subID,
		Amount:         plan.Price,
		Currency:       "USD",
	}, nil
}

func (s *subscriptionService) VerifyPayment(ctx context.Context, userID string, req *models.VerifyPaymentRequest) (*models.SubscriptionResponse, error) {
	// 1. Verify signature
	// Subscription verification uses payment_id + "|" + subscription_id
	// Order verification uses order_id + "|" + payment_id
	var data string
	var subID string

	if req.RazorpaySubscriptionID != "" {
		data = req.RazorpayPaymentID + "|" + req.RazorpaySubscriptionID
		subID = req.RazorpaySubscriptionID
	} else {
		data = req.RazorpayOrderID + "|" + req.RazorpayPaymentID
		subID = req.RazorpayOrderID
	}

	if !s.verifySignature(data, req.RazorpaySignature, s.razorpaySecret) {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid payment signature", "")
	}

	// 2. Find the subscription record for this payment
	sub, err := s.subRepo.GetBySubscriptionID(ctx, subID)
	if err != nil {
		return nil, err
	}

	if sub.UserID != userID {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "order does not belong to user", "")
	}

	isUpgrade := sub.Status == "upgrading"

	// 2. If it's an upgrade, we merge it into the existing active subscription
	if isUpgrade {
		// Use the new repository method to find the ACTUAL active subscription, skipping the 'upgrading' one
		activeSub, err := s.subRepo.GetByUserIDAndStatus(ctx, userID, models.SubscriptionStatusActive)
		if err != nil {
			fmt.Printf("[VerifyPayment] Upgrade failed: could not find active subscription to merge into for user %s: %v\n", userID, err)
			return nil, err
		}

		fmt.Printf("[VerifyPayment] Merging upgrade into active sub: %s\n", activeSub.SubscriptionID)

		// Rescue zero date if necessary from the ORIGINAL sub
		if activeSub.CurrentPeriodEnd.IsZero() {
			activeSub.CurrentPeriodEnd = time.Now().AddDate(0, 1, 0)
		}

		// Update the main active subscription with the new plan info
		activeSub.PlanID = sub.PlanID

		// Fetch full plan to get the real monthly price (not the prorated one)
		newPlan, _ := s.planService.GetPlanByID(ctx, sub.PlanID)
		if newPlan != nil {
			activeSub.Amount = newPlan.Price
		}

		activeSub.UpdatedAt = time.Now()
		if err := s.subRepo.Update(ctx, activeSub); err != nil {
			return nil, err
		}

		// Mark the upgrade PAYMENT order as completed/inactive so it doesn't show up in status
		sub.Status = "completed" // Instead of 'active', so it doesn't shadow the main sub
		sub.UpdatedAt = time.Now()
		s.subRepo.Update(ctx, sub)

		// Ensure we use the updated activeSub for credit calculation
		sub = activeSub
	} else {
		// Standard new subscription verification
		sub.Status = models.SubscriptionStatusActive
		sub.CurrentPeriodStart = time.Now()
		sub.CurrentPeriodEnd = time.Now().AddDate(0, 1, 0) // 1 month
		sub.UpdatedAt = time.Now()

		if err := s.subRepo.Update(ctx, sub); err != nil {
			return nil, err
		}
	}

	// 3. Add credits
	plan, err := s.planService.GetPlanByID(ctx, sub.PlanID)
	creditsToAdd := 1000
	if plan != nil {
		if isUpgrade {
			// Get current plan to calculate difference
			// Actually, let's just use the plan's credit amount if it's a fresh sub,
			// or the diff if it's an upgrade?
			// payment.md: "Additional credits corresponding to the higher plan are granted"
			// Usually this means NewPlan.Credits - OldPlan.Credits
			// But since it's mid-cycle, maybe it's prorated credits?
			// Simpler: Just give the difference.

			// We need the OLD plan ID. We didn't store it in the 'upgrading' record.
			// But we can assume the user had a plan.
			// Let's just grant the full credits of the new plan for simplicity, or the difference.
			// If Basic is 1000 and Pro is 3000, upgrading gives 2000 more.

			// For now, let's just use the full credits of the new plan as defined in plan model
			// or we can calculate the difference if we find the previous plan.

			// Let's assume the user gets the FULL credits of the new plan minus what they already got?
			// That's complex. Let's just add the 'Credits' from the plan.
			creditsToAdd = plan.Credits
		} else {
			creditsToAdd = plan.Credits
		}
	}

	user, err := s.userService.GetOrCreateUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure user existence: %w", err)
	}

	creditsResp, err := s.creditsService.AddCredits(ctx, &models.AddCreditsRequest{
		UserID: user.UserID,
		Amount: creditsToAdd,
	})
	if err != nil {
		return nil, err
	}

	// Send confirmation email (fire-and-forget)
	if plan != nil {
		nextBilling := sub.CurrentPeriodEnd
		if nextBilling.IsZero() {
			nextBilling = time.Now().AddDate(0, 1, 0)
		}
		go s.emailService.SendSubscriptionConfirmation(sub.Email, sub.Email, plan.Name, creditsToAdd, nextBilling)
	}

	return &models.SubscriptionResponse{
		Message:          "Payment verified and plan updated",
		Status:           models.SubscriptionStatusActive,
		CreditsAdded:     creditsToAdd,
		RemainingCredits: creditsResp.Credits,
	}, nil
}

func (s *subscriptionService) CalculateUpgradePrice(ctx context.Context, userID, newPlanID string) (int, error) {
	// 1. Get current subscription
	sub, err := s.subRepo.GetByUserID(ctx, userID)
	if err != nil {
		return 0, err
	}

	if sub.Status != models.SubscriptionStatusActive {
		return 0, apperrors.NewAppError(apperrors.ErrBadRequest, 400, "no active subscription to upgrade", "")
	}

	// 2. Get new plan details
	newPlan, err := s.planService.GetPlanByID(ctx, newPlanID)
	if err != nil {
		return 0, err
	}

	// 3. Get current plan details (to check if it's actually an upgrade)
	currentPlan, err := s.planService.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return 0, err
	}

	if newPlan.Price <= currentPlan.Price {
		return 0, apperrors.NewAppError(apperrors.ErrBadRequest, 400, "new plan price must be higher for upgrade", "")
	}

	// 4. Calculate proration
	// Time remaining in current period
	now := time.Now()
	totalDuration := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart)
	remainingDuration := sub.CurrentPeriodEnd.Sub(now)

	if remainingDuration <= 0 {
		return newPlan.Price, nil
	}

	// Prorated amount = (New Plan Price - Current Plan Price) * (Remaining Time / Total Time)
	priceDiff := newPlan.Price - currentPlan.Price
	proratedDiff := int(float64(priceDiff) * (remainingDuration.Hours() / totalDuration.Hours()))

	fmt.Printf("[CalculateUpgradePrice] User: %s, CurrentPlan: %s ($%d), NewPlan: %s ($%d), Time: %.2f/%.2f hrs, Price: %d\n",
		userID, currentPlan.PlanID, currentPlan.Price, newPlan.PlanID, newPlan.Price, remainingDuration.Hours(), totalDuration.Hours(), proratedDiff)

	// Minimum charge (e.g., $1.00) to avoid processing tiny amounts if near end of cycle
	if proratedDiff < 100 {
		proratedDiff = 100
	}

	return proratedDiff, nil
}

func (s *subscriptionService) CreateUpgradeOrder(ctx context.Context, userID, newPlanID string) (*models.SubscriptionResponse, error) {
	fmt.Printf("[CreateUpgradeOrder] Starting for User: %s, NewPlan: %s\n", userID, newPlanID)
	price, err := s.CalculateUpgradePrice(ctx, userID, newPlanID)
	if err != nil {
		fmt.Printf("[CreateUpgradeOrder] CalculateUpgradePrice failed: %v\n", err)
		return nil, err
	}

	// Create Razorpay order for the prorated amount
	params := map[string]interface{}{
		"amount":   price,
		"currency": "USD",
		"receipt":  fmt.Sprintf("upg_%d", time.Now().Unix()),
		"notes": map[string]string{
			"type":      "upgrade",
			"newPlanID": newPlanID,
			"userID":    userID,
		},
	}

	fmt.Printf("[CreateUpgradeOrder] Razorpay Params: %+v\n", params)

	body, err := s.razorpay.CreateOrder(params)
	if err != nil {
		fmt.Printf("[CreateUpgradeOrder] Razorpay Error: %v\n", err)
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create razorpay upgrade order", err.Error())
	}

	orderID := body["id"].(string)
	fmt.Printf("[CreateUpgradeOrder] Razorpay Order Created: %s\n", orderID)

	// Fetch current sub to get email
	currentSub, _ := s.subRepo.GetByUserID(ctx, userID)
	email := ""
	if currentSub != nil {
		email = currentSub.Email
	}

	// Store a pending upgrade record
	upgradeSub := &models.Subscription{
		SubscriptionID: orderID,
		UserID:         userID,
		Email:          email,
		Status:         "upgrading", // Special status
		PlanID:         newPlanID,
		Amount:         price,
		Currency:       "USD",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.subRepo.Create(ctx, upgradeSub); err != nil {
		return nil, err
	}

	return &models.SubscriptionResponse{
		Message:  "Upgrade order created successfully",
		OrderID:  orderID,
		Amount:   price,
		Currency: "USD",
	}, nil
}

func (s *subscriptionService) DowngradeSubscription(ctx context.Context, userID, newPlanID string) error {
	// 1. Get current subscription
	sub, err := s.subRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	if sub.Status != models.SubscriptionStatusActive && sub.Status != models.SubscriptionStatusPastDue {
		return apperrors.NewAppError(apperrors.ErrBadRequest, 400, "no active subscription to downgrade", "")
	}

	// 2. Get new plan details
	newPlan, err := s.planService.GetPlanByID(ctx, newPlanID)
	if err != nil {
		return err
	}

	// 3. Mark for downgrade
	sub.PendingPlanID = newPlan.PlanID
	sub.PlanChangeDate = &sub.CurrentPeriodEnd
	sub.UpdatedAt = time.Now()

	return s.subRepo.Update(ctx, sub)
}

func (s *subscriptionService) GetSubscriptionStatus(ctx context.Context, userID string) (*models.Subscription, error) {
	return s.subRepo.GetByUserID(ctx, userID)
}

func (s *subscriptionService) CancelSubscription(ctx context.Context, userID string) error {
	// 1. Get current subscription
	sub, err := s.subRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	// 2. Only active or past_due can be cancelled
	if sub.Status != models.SubscriptionStatusActive && sub.Status != models.SubscriptionStatusPastDue {
		return apperrors.NewAppError(apperrors.ErrBadRequest, 400, "no active subscription to cancel", "")
	}

	if sub.CancelAtCycleEnd && sub.Status == models.SubscriptionStatusActive {
		return nil
	}

	// 3. Ask Razorpay to cancel at the end of the billing cycle.
	if _, err := s.razorpay.CancelSubscription(sub.SubscriptionID, map[string]interface{}{
		"cancel_at_cycle_end": 1,
	}); err != nil {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to schedule razorpay cancellation", err.Error())
	}

	// 4. Persist scheduled-cancellation metadata locally while keeping access active.
	now := time.Now()
	sub.CancelAtCycleEnd = true
	sub.CancelScheduledAt = &now
	sub.UpdatedAt = now

	return s.subRepo.Update(ctx, sub)
}

func (s *subscriptionService) GetAllSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	return s.subRepo.GetAll(ctx)
}

func (s *subscriptionService) GetActiveSubscriptionCount(ctx context.Context) (int64, error) {
	return s.subRepo.CountActive(ctx)
}

func (s *subscriptionService) ListAdminSubscriptions(ctx context.Context, query models.AdminSubscriptionQuery) (*models.AdminSubscriptionListResponse, error) {
	subs, total, err := s.subRepo.List(ctx, query)
	if err != nil {
		return nil, err
	}

	converted, err := s.decorateSubscriptions(ctx, subs)
	if err != nil {
		return nil, err
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	skip := query.Skip
	if skip < 0 {
		skip = 0
	}

	return &models.AdminSubscriptionListResponse{
		Message:       "Subscriptions retrieved successfully",
		Subscriptions: converted,
		Total:         total,
		Limit:         limit,
		Skip:          skip,
	}, nil
}

func (s *subscriptionService) GetAdminSubscription(ctx context.Context, subscriptionID string) (*models.AdminSubscriptionDetailResponse, error) {
	sub, err := s.subRepo.GetBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	converted, err := s.decorateSubscriptions(ctx, []models.Subscription{*sub})
	if err != nil {
		return nil, err
	}
	if len(converted) == 0 {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "subscription not found", "")
	}

	return &models.AdminSubscriptionDetailResponse{
		Message:      "Subscription retrieved successfully",
		Subscription: converted[0],
	}, nil
}

func (s *subscriptionService) ReconcileAdminSubscription(ctx context.Context, subscriptionID string) (*models.AdminSubscriptionDetailResponse, error) {
	sub, err := s.subRepo.GetBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	remote, err := s.razorpay.FetchSubscription(subscriptionID)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to fetch razorpay subscription", err.Error())
	}

	if status, ok := remote["status"].(string); ok && status != "" {
		sub.Status = normalizeSubscriptionStatus(status)
	}
	if cancelAtCycleEnd, ok := asBool(remote["cancel_at_cycle_end"]); ok {
		sub.CancelAtCycleEnd = cancelAtCycleEnd
	}
	if currentStart, ok := parseRemoteTime(remote["current_start"]); ok {
		sub.CurrentPeriodStart = currentStart
	}
	if currentEnd, ok := parseRemoteTime(remote["current_end"]); ok {
		sub.CurrentPeriodEnd = currentEnd
	}
	if endedAt, ok := parseRemoteTime(remote["ended_at"]); ok {
		sub.CancelledAt = &endedAt
	}
	if sub.CancelAtCycleEnd && sub.CancelScheduledAt == nil {
		now := time.Now()
		sub.CancelScheduledAt = &now
	}
	sub.UpdatedAt = time.Now()

	if err := s.subRepo.Update(ctx, sub); err != nil {
		return nil, err
	}

	converted, err := s.decorateSubscriptions(ctx, []models.Subscription{*sub})
	if err != nil {
		return nil, err
	}

	return &models.AdminSubscriptionDetailResponse{
		Message:      "Subscription reconciled successfully",
		Subscription: converted[0],
	}, nil
}

func (s *subscriptionService) decorateSubscriptions(ctx context.Context, subs []models.Subscription) ([]models.AdminSubscription, error) {
	plans, err := s.planService.GetAllPlans(ctx)
	if err != nil {
		return nil, err
	}

	planNames := make(map[string]struct {
		Name         string
		RazorpayPlan string
	})
	for _, plan := range plans {
		planNames[plan.PlanID] = struct {
			Name         string
			RazorpayPlan string
		}{Name: plan.Name, RazorpayPlan: plan.RazorpayPlanID}
	}

	result := make([]models.AdminSubscription, 0, len(subs))
	for _, sub := range subs {
		item := models.AdminSubscription{
			ID:                 sub.ID,
			UserID:             sub.UserID,
			Email:              sub.Email,
			PlanID:             sub.PlanID,
			SubscriptionID:     sub.SubscriptionID,
			Status:             sub.Status,
			Amount:             sub.Amount,
			Currency:           sub.Currency,
			CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd:   sub.CurrentPeriodEnd,
			GracePeriodEnd:     sub.GracePeriodEnd,
			CancelAtCycleEnd:   sub.CancelAtCycleEnd,
			CancelScheduledAt:  sub.CancelScheduledAt,
			CancelledAt:        sub.CancelledAt,
			PendingPlanID:      sub.PendingPlanID,
			PlanChangeDate:     sub.PlanChangeDate,
			CreatedAt:          sub.CreatedAt,
			UpdatedAt:          sub.UpdatedAt,
		}
		if info, ok := planNames[sub.PlanID]; ok {
			item.PlanName = info.Name
			item.PlanRazorpayID = info.RazorpayPlan
		}
		result = append(result, item)
	}

	return result, nil
}

func normalizeSubscriptionStatus(raw string) models.SubscriptionStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "created":
		return models.SubscriptionStatusCreated
	case "active":
		return models.SubscriptionStatusActive
	case "past_due":
		return models.SubscriptionStatusPastDue
	case "cancelled":
		return models.SubscriptionStatusCancelled
	case "expired":
		return models.SubscriptionStatusExpired
	default:
		return models.SubscriptionStatus(raw)
	}
}

func parseRemoteTime(value interface{}) (time.Time, bool) {
	switch v := value.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return v, true
	case float64:
		if v <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(v), 0).UTC(), true
	case int64:
		if v <= 0 {
			return time.Time{}, false
		}
		return time.Unix(v, 0).UTC(), true
	case int:
		if v <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(v), 0).UTC(), true
	case string:
		if v == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return parsed, true
		}
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			return time.Unix(parsed, 0).UTC(), true
		}
	}
	return time.Time{}, false
}

func asBool(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	case int:
		return v != 0, true
	case int64:
		return v != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

func (s *subscriptionService) HandleWebhookRaw(ctx context.Context, rawPayload []byte, signature string) error {
	if s.webhookSecret == "" {
		return apperrors.NewAppError(apperrors.ErrInternalServer, 500, "webhook secret is not configured", "")
	}
	if signature == "" {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "missing webhook signature", "")
	}
	if !s.verifySignature(string(rawPayload), signature, s.webhookSecret) {
		return apperrors.NewAppError(apperrors.ErrUnauthorized, 401, "invalid webhook signature", "")
	}

	var payload models.WebhookPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid webhook payload", err.Error())
	}

	return s.HandleWebhook(ctx, &payload, signature)
}

func (s *subscriptionService) HandleWebhook(ctx context.Context, payload *models.WebhookPayload, signature string) error {
	_ = signature

	switch payload.Event {
	case "subscription.charged":
		// This event is triggered when a recurring payment is successfully charged
		payloadData, ok := payload.Payload["subscription"].(map[string]interface{})
		if !ok {
			return nil
		}
		entity, ok := payloadData["entity"].(map[string]interface{})
		if !ok {
			return nil
		}
		subID := entity["id"].(string)

		fmt.Printf("[Webhook] Subscription charged: %s\n", subID)

		sub, err := s.subRepo.GetBySubscriptionID(ctx, subID)
		if err != nil {
			fmt.Printf("[Webhook] Subscription not found in DB: %s\n", subID)
			return nil // Or handle accordingly
		}

		plan, err := s.planService.GetPlanByID(ctx, sub.PlanID)
		if err != nil {
			return err
		}

		// Add credits for the new billing cycle
		fmt.Printf("[Webhook] Adding %d credits for user %s\n", plan.Credits, sub.UserID)
		creditsReq := &models.AddCreditsRequest{
			UserID: sub.UserID,
			Amount: plan.Credits,
		}
		_, err = s.creditsService.AddCredits(ctx, creditsReq)
		if err != nil {
			return err
		}

		// Send confirmation email (fire-and-forget, don't fail on email error)
		nextBilling := time.Now().AddDate(0, 1, 0)
		go s.emailService.SendSubscriptionConfirmation(sub.Email, sub.Email, plan.Name, plan.Credits, nextBilling)
		return nil

	case "subscription.halted":
		// All Razorpay retries have failed — expire the subscription and clear credits
		subData, ok := payload.Payload["subscription"].(map[string]interface{})
		if !ok {
			return nil
		}
		entity, ok := subData["entity"].(map[string]interface{})
		if !ok {
			return nil
		}
		subID := entity["id"].(string)
		fmt.Printf("[Webhook] Subscription halted (all retries failed): %s\n", subID)

		sub, err := s.subRepo.GetBySubscriptionID(ctx, subID)
		if err != nil {
			return nil
		}

		// Mark subscription as expired
		if err := s.subRepo.UpdateStatus(ctx, subID, models.SubscriptionStatusExpired); err != nil {
			return err
		}

		// Clear the user's credits by deducting their full balance
		fmt.Printf("[Webhook] Clearing credits for user %s after subscription halted\n", sub.UserID)
		balance, err := s.creditsService.GetBalance(ctx, sub.UserID)
		if err == nil && balance.Credits > 0 {
			deductReq := &models.DeductCreditsRequest{
				UserID: sub.UserID,
				Amount: balance.Credits,
			}
			_, err = s.creditsService.DeductCredits(ctx, deductReq)
		}

		// Send expired email
		go s.emailService.SendSubscriptionExpired(sub.Email, sub.Email)
		return err

	case "subscription.cancelled":
		subData := payload.Payload["subscription"].(map[string]interface{})["entity"].(map[string]interface{})
		subID := subData["id"].(string)
		sub, err := s.subRepo.GetBySubscriptionID(ctx, subID)
		if err != nil {
			return err
		}

		now := time.Now()
		sub.Status = models.SubscriptionStatusCancelled
		sub.CancelledAt = &now
		sub.UpdatedAt = now
		if err := s.subRepo.Update(ctx, sub); err != nil {
			return err
		}

		// Send cancellation email
		accessUntil := time.Now().AddDate(0, 0, 30) // approximate; use sub.CurrentPeriodEnd if stored
		if !sub.CurrentPeriodEnd.IsZero() {
			accessUntil = sub.CurrentPeriodEnd
		}
		go s.emailService.SendCancellationConfirmation(sub.Email, sub.Email, accessUntil)
		return nil

	case "payment.failed":
		// Mark as past_due and notify user
		paymentData := payload.Payload["payment"].(map[string]interface{})["entity"].(map[string]interface{})
		if subID, ok := paymentData["subscription_id"].(string); ok {
			if err := s.subRepo.UpdateStatus(ctx, subID, models.SubscriptionStatusPastDue); err != nil {
				return err
			}
			// Send payment failed email
			sub, err := s.subRepo.GetBySubscriptionID(ctx, subID)
			if err == nil {
				retryIn := time.Now().AddDate(0, 0, 3)
				go s.emailService.SendPaymentFailed(sub.Email, sub.Email, retryIn)
			}
		}
	}

	return nil
}

func (s *subscriptionService) verifySignature(data, signature, secret string) bool {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	expectedSignature := h.Sum(nil)
	providedSignature, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	return hmac.Equal(expectedSignature, providedSignature)
}
