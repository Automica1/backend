// internal/models/credits.go
package models

import (
	"errors"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Credits struct {
	ID      primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	UserID  string             `bson:"userId" json:"userId"`
	Credits int                `bson:"credits" json:"credits"`
}

type AddCreditsRequest struct {
	UserID string `json:"userId" validate:"required"`
	Amount int    `json:"amount" validate:"required,min=1"`
}

type DeductCreditsRequest struct {
	UserID string `json:"userId" validate:"required"`
	Amount int    `json:"amount" validate:"required,min=1"` // Added amount field
}

func (r *AddCreditsRequest) Validate() error {
	if r.UserID == "" {
		return errors.New("userId is required")
	}
	if r.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}

func (r *DeductCreditsRequest) Validate() error {
	if r.UserID == "" {
		return errors.New("userId is required")
	}
	if r.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}