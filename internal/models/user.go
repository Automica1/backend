// internal/models/user.go
package models

import (
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID                                  primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	UserID                              string             `bson:"userId" json:"userId"`
	Email                               string             `bson:"email" json:"email"`
	BillingCurrency                     string             `bson:"billingCurrency,omitempty" json:"billingCurrency,omitempty"`
	BetaFeedbackMonthlyRefundCapOverride *int              `bson:"betaFeedbackMonthlyRefundCapOverride,omitempty" json:"betaFeedbackMonthlyRefundCapOverride,omitempty"`
	BetaFeedbackRefundBudgetResetAt     *time.Time         `bson:"betaFeedbackRefundBudgetResetAt,omitempty" json:"betaFeedbackRefundBudgetResetAt,omitempty"`
	IsActive                            bool               `bson:"isActive" json:"isActive"`
	CreatedAt                           time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt                           time.Time          `bson:"updatedAt" json:"updatedAt"`
}

type RegisterUserRequest struct {
	UserID string `json:"userId" validate:"required"`
	Email  string `json:"email" validate:"required,email"`
}

func (r *RegisterUserRequest) Validate() error {
	if strings.TrimSpace(r.UserID) == "" {
		return errors.New("userId is required")
	}
	if strings.TrimSpace(r.Email) == "" {
		return errors.New("email is required")
	}
	if !isValidEmail(r.Email) {
		return errors.New("invalid email format")
	}
	return nil
}

func isValidEmail(email string) bool {
	// Basic email validation - in production, use a proper validation library
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}
