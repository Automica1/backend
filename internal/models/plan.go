// internal/models/plan.go
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PlanCurrencyPricing struct {
	Amount         int    `bson:"amount" json:"amount"`
	RazorpayPlanID string `bson:"razorpayPlanId" json:"razorpayPlanId"`
}

type Plan struct {
	ID             primitive.ObjectID             `bson:"_id,omitempty" json:"id,omitempty"`
	PlanID         string                         `bson:"planId" json:"planId"`
	RazorpayPlanID string                         `bson:"razorpayPlanId" json:"razorpayPlanId"`
	Name           string                         `bson:"name" json:"name"`
	Description    string                         `bson:"description" json:"description"`
	Price          int                            `bson:"price" json:"price"` // legacy USD cents; resolved per currency in API
	Credits        int                            `bson:"credits" json:"credits"`
	IsActive       bool                           `bson:"isActive" json:"isActive"`
	Pricing        map[string]PlanCurrencyPricing `bson:"pricing,omitempty" json:"pricing,omitempty"`
	Currency       string                         `bson:"-" json:"currency,omitempty"` // set on resolved API responses only
	CreatedAt      time.Time                      `bson:"createdAt" json:"createdAt"`
	UpdatedAt      time.Time                      `bson:"updatedAt" json:"updatedAt"`
}

type CreatePlanRequest struct {
	PlanID         string                         `json:"planId" validate:"required"`
	RazorpayPlanID string                         `json:"razorpayPlanId"`
	Name           string                         `json:"name" validate:"required"`
	Description    string                         `json:"description"`
	Price          int                            `json:"price" validate:"min=0"`
	Credits        int                            `json:"credits" validate:"required,min=0"`
	IsActive       bool                           `json:"isActive"`
	Pricing        map[string]PlanCurrencyPricing `json:"pricing,omitempty"`
	PriceUSD       int                            `json:"priceUsd,omitempty"`
	PriceINR       int                            `json:"priceInr,omitempty"`
	RazorpayPlanIDUSD string                      `json:"razorpayPlanIdUsd,omitempty"`
	RazorpayPlanIDINR string                      `json:"razorpayPlanIdInr,omitempty"`
}

type UpdatePlanRequest struct {
	RazorpayPlanID    *string                         `json:"razorpayPlanId,omitempty"`
	Name              *string                         `json:"name,omitempty"`
	Description       *string                         `json:"description,omitempty"`
	Price             *int                            `json:"price,omitempty"`
	Credits           *int                            `json:"credits,omitempty"`
	IsActive          *bool                           `json:"isActive,omitempty"`
	Pricing           map[string]PlanCurrencyPricing `json:"pricing,omitempty"`
	PriceUSD          *int                            `json:"priceUsd,omitempty"`
	PriceINR          *int                            `json:"priceInr,omitempty"`
	RazorpayPlanIDUSD *string                         `json:"razorpayPlanIdUsd,omitempty"`
	RazorpayPlanIDINR *string                         `json:"razorpayPlanIdInr,omitempty"`
}
