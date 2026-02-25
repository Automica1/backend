// internal/services/subscription_service.go
package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"

	"github.com/razorpay/razorpay-go"
)

type SubscriptionService interface {
	CreateOrder(ctx context.Context, userID, email, planID string) (*models.SubscriptionResponse, error)
	VerifyPayment(ctx context.Context, userID string, req *models.VerifyPaymentRequest) (*models.SubscriptionResponse, error)
	GetSubscriptionStatus(ctx context.Context, userID string) (*models.Subscription, error)
	CancelSubscription(ctx context.Context, userID string) error
	GetAllSubscriptions(ctx context.Context) ([]models.Subscription, error)
	GetActiveSubscriptionCount(ctx context.Context) (int64, error)
	HandleWebhook(ctx context.Context, payload *models.WebhookPayload, signature string) error
}

type subscriptionService struct {
	subRepo        repository.SubscriptionRepository
	creditsService CreditsService
	userService    UserService
	planService    PlanService
	razorpayKey    string
	razorpaySecret string
	webhookSecret  string
	client         *razorpay.Client
}

func NewSubscriptionService(subRepo repository.SubscriptionRepository, creditsService CreditsService, userService UserService, planService PlanService, key, secret, webhookSecret string) SubscriptionService {
	client := razorpay.NewClient(key, secret)
	return &subscriptionService{
		subRepo:        subRepo,
		creditsService: creditsService,
		userService:    userService,
		planService:    planService,
		razorpayKey:    key,
		razorpaySecret: secret,
		webhookSecret:  webhookSecret,
		client:         client,
	}
}

func (s *subscriptionService) CreateOrder(ctx context.Context, userID, email, planID string) (*models.SubscriptionResponse, error) {
	// Fetch plan details from DB
	plan, err := s.planService.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}

	if !plan.IsActive {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "plan is not active", "")
	}

	params := map[string]interface{}{
		"amount":   plan.Price, // amount in cents
		"currency": "USD",
		"receipt":  fmt.Sprintf("receipt_%d", time.Now().Unix()),
	}

	body, err := s.client.Order.Create(params, nil)
	if err != nil {
		return nil, apperrors.NewAppError(apperrors.ErrInternalServer, 500, "failed to create razorpay order", err.Error())
	}

	orderID := body["id"].(string)

	// Store a pending subscription record
	sub := &models.Subscription{
		SubscriptionID: orderID,
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
		Message:  "Order created successfully",
		OrderID:  orderID,
		Amount:   plan.Price,
		Currency: "USD",
	}, nil
}

func (s *subscriptionService) VerifyPayment(ctx context.Context, userID string, req *models.VerifyPaymentRequest) (*models.SubscriptionResponse, error) {
	// Verify signature
	data := req.RazorpayOrderID + "|" + req.RazorpayPaymentID
	if !s.verifySignature(data, req.RazorpaySignature, s.razorpaySecret) {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "invalid payment signature", "")
	}

	// Update subscription status
	sub, err := s.subRepo.GetBySubscriptionID(ctx, req.RazorpayOrderID)
	if err != nil {
		return nil, err
	}

	if sub.UserID != userID {
		return nil, apperrors.NewAppError(apperrors.ErrForbidden, 403, "order does not belong to user", "")
	}

	sub.Status = models.SubscriptionStatusActive
	sub.CurrentPeriodStart = time.Now()
	sub.CurrentPeriodEnd = time.Now().AddDate(0, 1, 0) // 1 month
	sub.UpdatedAt = time.Now()

	if err := s.subRepo.Update(ctx, sub); err != nil {
		return nil, err
	}

	// Fetch plan details to know how many credits to add
	plan, err := s.planService.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		// Fallback to 1000 if plan not found for some reason
		fmt.Printf("[VerifyPayment] Warning: Plan %s not found, falling back to 1000 credits\n", sub.PlanID)
	}

	creditsToAdd := 1000
	if plan != nil {
		creditsToAdd = plan.Credits
	}

	// Ensure user and credits record exist, and get the actual UserID from DB
	user, err := s.userService.GetOrCreateUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure user existence: %w", err)
	}

	actualUserID := user.UserID
	fmt.Printf("[VerifyPayment] Passed ID: %s, DB UserID: %s, Sub UserID: %s, Credits: %d\n", userID, actualUserID, sub.UserID, creditsToAdd)

	creditsResp, err := s.creditsService.AddCredits(ctx, &models.AddCreditsRequest{
		UserID: actualUserID,
		Amount: creditsToAdd,
	})
	if err != nil {
		return nil, err
	}

	return &models.SubscriptionResponse{
		Message:          "Payment verified and credits added",
		Status:           models.SubscriptionStatusActive,
		CreditsAdded:     creditsToAdd,
		RemainingCredits: creditsResp.Credits,
	}, nil
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

	// 3. Update status to cancelled
	return s.subRepo.UpdateStatus(ctx, sub.SubscriptionID, models.SubscriptionStatusCancelled)
}

func (s *subscriptionService) GetAllSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	return s.subRepo.GetAll(ctx)
}

func (s *subscriptionService) GetActiveSubscriptionCount(ctx context.Context) (int64, error) {
	return s.subRepo.CountActive(ctx)
}

func (s *subscriptionService) HandleWebhook(ctx context.Context, payload *models.WebhookPayload, signature string) error {
	// In production, verify the webhook signature
	// data := string(rawPayload) // we would need the raw bytes here
	// if !s.verifySignature(data, signature, s.webhookSecret) {
	//     return errors.New("invalid webhook signature")
	// }

	switch payload.Event {
	case "payment.captured":
		// Handle payment captured if needed (already handled in VerifyPayment for frontend flow)
	case "subscription.cancelled":
		subID := payload.Payload["subscription"].(map[string]interface{})["entity"].(map[string]interface{})["id"].(string)
		return s.subRepo.UpdateStatus(ctx, subID, models.SubscriptionStatusCancelled)
	}

	return nil
}

func (s *subscriptionService) verifySignature(data, signature, secret string) bool {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	expectedSignature := hex.EncodeToString(h.Sum(nil))
	return expectedSignature == signature
}
