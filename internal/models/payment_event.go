package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PaymentEvent records a processed Razorpay payment for idempotency.
type PaymentEvent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	PaymentID      string             `bson:"paymentId" json:"paymentId"`
	EventType      string             `bson:"eventType" json:"eventType"`
	UserID         string             `bson:"userId,omitempty" json:"userId,omitempty"`
	SubscriptionID string             `bson:"subscriptionId,omitempty" json:"subscriptionId,omitempty"`
	CreditsAdded   int                `bson:"creditsAdded,omitempty" json:"creditsAdded,omitempty"`
	ProcessedAt    time.Time          `bson:"processedAt" json:"processedAt"`
}
