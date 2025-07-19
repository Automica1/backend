// internal/models/token.go
package models

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type CreditToken struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Token       string             `bson:"token" json:"token"`
	Credits     int                `bson:"credits" json:"credits"`
	CreatedBy   string             `bson:"createdBy" json:"createdBy"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	ExpiresAt   time.Time          `bson:"expiresAt" json:"expiresAt"`
	IsUsed      bool               `bson:"isUsed" json:"isUsed"`
	UsedBy      string             `bson:"usedBy,omitempty" json:"usedBy,omitempty"`
	UsedAt      *time.Time         `bson:"usedAt,omitempty" json:"usedAt,omitempty"`
	Description string             `bson:"description,omitempty" json:"description,omitempty"`
}

type GenerateTokenRequest struct {
	Credits     int    `json:"credits" validate:"required,min=1"`
	Description string `json:"description,omitempty"`
}

type RedeemTokenRequest struct {
	Token string `json:"token" validate:"required"`
}

type TokenResponse struct {
	Message           string     `json:"message"`
	Token             string     `json:"token,omitempty"`
	Credits           int        `json:"credits"`
	RemainingCredits  int        `json:"remainingCredits,omitempty"`  // Add this field
	ExpiresAt         time.Time  `json:"expiresAt,omitempty"`
	UsedAt            *time.Time `json:"usedAt,omitempty"`
	Description       string     `json:"description,omitempty"`
}

func (r *GenerateTokenRequest) Validate() error {
	if r.Credits <= 0 {
		return errors.New("credits must be positive")
	}
	return nil
}

func (r *RedeemTokenRequest) Validate() error {
	if r.Token == "" {
		return errors.New("token is required")
	}
	return nil
}

// GenerateToken creates a new random token
func GenerateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// IsExpired checks if the token has expired
func (t *CreditToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}