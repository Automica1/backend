// internal/models/subscription.go
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SubscriptionStatus string

const (
	SubscriptionStatusCreated   SubscriptionStatus = "created"
	SubscriptionStatusActive    SubscriptionStatus = "active"
	SubscriptionStatusPastDue   SubscriptionStatus = "past_due"
	SubscriptionStatusCancelled SubscriptionStatus = "cancelled"
	SubscriptionStatusExpired   SubscriptionStatus = "expired"
)

type Subscription struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SubscriptionID     string             `bson:"subscriptionId" json:"subscriptionId"` // Razorpay Subscription ID or Order ID
	UserID             string             `bson:"userId" json:"userId"`
	Email              string             `bson:"email" json:"email"`
	Status             SubscriptionStatus `bson:"status" json:"status"`
	PlanID             string             `bson:"planId" json:"planId"`
	Amount             int                `bson:"amount" json:"amount"` // in paise
	Currency           string             `bson:"currency" json:"currency"`
	CurrentPeriodStart time.Time          `bson:"currentPeriodStart" json:"currentPeriodStart"`
	CurrentPeriodEnd   time.Time          `bson:"currentPeriodEnd" json:"currentPeriodEnd"`
	GracePeriodEnd     *time.Time         `bson:"gracePeriodEnd,omitempty" json:"gracePeriodEnd,omitempty"`
	PendingPlanID      string             `bson:"pendingPlanId,omitempty" json:"pendingPlanId,omitempty"`
	PlanChangeDate     *time.Time         `bson:"planChangeDate,omitempty" json:"planChangeDate,omitempty"`
	CreatedAt          time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt          time.Time          `bson:"updatedAt" json:"updatedAt"`
}

type CreateSubscriptionRequest struct {
	PlanID string `json:"planId" validate:"required"`
}

type VerifyPaymentRequest struct {
	RazorpayPaymentID string `json:"razorpay_payment_id" validate:"required"`
	RazorpayOrderID   string `json:"razorpay_order_id" validate:"required"`
	RazorpaySignature string `json:"razorpay_signature" validate:"required"`
}

type SubscriptionResponse struct {
	Message          string             `json:"message"`
	SubscriptionID   string             `json:"subscriptionId,omitempty"`
	OrderID          string             `json:"orderId,omitempty"`
	Amount           int                `json:"amount,omitempty"`
	Currency         string             `json:"currency,omitempty"`
	Status           SubscriptionStatus `json:"status,omitempty"`
	CreditsAdded     int                `json:"creditsAdded,omitempty"`
	RemainingCredits int                `json:"remainingCredits,omitempty"`
}

type WebhookPayload struct {
	Domain    string                 `json:"domain"`
	Entity    string                 `json:"entity"`
	Account   string                 `json:"account_id"`
	Event     string                 `json:"event"`
	Payload   map[string]interface{} `json:"payload"`
	CreatedAt int64                  `json:"created_at"`
}
