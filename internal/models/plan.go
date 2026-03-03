// internal/models/plan.go
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Plan struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	PlanID         string             `bson:"planId" json:"planId"` // e.g., "Basic", "Professional"
	RazorpayPlanID string             `bson:"razorpayPlanId" json:"razorpayPlanId"`
	Name           string             `bson:"name" json:"name"`
	Description    string             `bson:"description" json:"description"`
	Price          int                `bson:"price" json:"price"`     // Amount in cents
	Credits        int                `bson:"credits" json:"credits"` // Credits awarded per month
	IsActive       bool               `bson:"isActive" json:"isActive"`
	CreatedAt      time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updatedAt" json:"updatedAt"`
}

type CreatePlanRequest struct {
	PlanID         string `json:"planId" validate:"required"`
	RazorpayPlanID string `json:"razorpayPlanId" validate:"required"`
	Name           string `json:"name" validate:"required"`
	Description    string `json:"description"`
	Price          int    `json:"price" validate:"required,min=0"`
	Credits        int    `json:"credits" validate:"required,min=0"`
	IsActive       bool   `json:"isActive"`
}

type UpdatePlanRequest struct {
	RazorpayPlanID *string `json:"razorpayPlanId,omitempty"`
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	Price          *int    `json:"price,omitempty"`
	Credits        *int    `json:"credits,omitempty"`
	IsActive       *bool   `json:"isActive,omitempty"`
}
