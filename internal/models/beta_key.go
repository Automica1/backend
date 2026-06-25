package models

import (
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Services that currently support beta routing.
var SupportedBetaServices = map[string]bool{
	"signature-verification": true,
}

func IsBetaServiceSupported(serviceName string) bool {
	return SupportedBetaServices[serviceName]
}

type BetaKey struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	KeyHash           string             `bson:"keyHash" json:"-"`
	KeyPrefix         string             `bson:"keyPrefix" json:"keyPrefix"`
	ServiceName       string             `bson:"serviceName" json:"serviceName"`
	Label             string             `bson:"label" json:"label"`
	AssignedUserEmail string             `bson:"assignedUserEmail" json:"assignedUserEmail"`
	CreatedBy         string             `bson:"createdBy" json:"createdBy"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	ExpiresAt   *time.Time         `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
	RevokedAt   *time.Time         `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
	IsActive    bool               `bson:"isActive" json:"isActive"`
	UsageCount  int64              `bson:"usageCount" json:"usageCount"`
	LastUsedAt  *time.Time         `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
}

type GenerateBetaKeyRequest struct {
	ServiceName       string `json:"serviceName"`
	Label             string `json:"label"`
	AssignedUserEmail string `json:"assignedUserEmail"`
	ExpiresInDays     *int   `json:"expiresInDays,omitempty"`
}

type GenerateBetaKeyResponse struct {
	Message           string     `json:"message"`
	BetaKey           string     `json:"betaKey"`
	KeyPrefix         string     `json:"keyPrefix"`
	ServiceName       string     `json:"serviceName"`
	Label             string     `json:"label"`
	AssignedUserEmail string     `json:"assignedUserEmail"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type BetaKeyListResponse struct {
	Message string    `json:"message"`
	Keys    []BetaKey `json:"keys"`
	Total   int       `json:"total"`
}

type RevokeBetaKeyResponse struct {
	Message string `json:"message"`
	ID      string `json:"id"`
}

func (r *GenerateBetaKeyRequest) Validate() error {
	r.ServiceName = strings.TrimSpace(r.ServiceName)
	r.Label = strings.TrimSpace(r.Label)
	r.AssignedUserEmail = strings.TrimSpace(strings.ToLower(r.AssignedUserEmail))

	if r.ServiceName == "" {
		return errors.New("serviceName is required")
	}
	if !IsBetaServiceSupported(r.ServiceName) {
		return errors.New("beta is not supported for this service")
	}
	if r.AssignedUserEmail == "" {
		return errors.New("assignedUserEmail is required")
	}
	if !isValidEmail(r.AssignedUserEmail) {
		return errors.New("invalid assignedUserEmail format")
	}
	if r.Label == "" {
		return errors.New("label is required")
	}
	if len(r.Label) > 100 {
		return errors.New("label must be 100 characters or less")
	}
	if r.ExpiresInDays != nil {
		if *r.ExpiresInDays < 1 || *r.ExpiresInDays > 365 {
			return errors.New("expiresInDays must be between 1 and 365")
		}
	}
	return nil
}

func (b *BetaKey) IsExpired() bool {
	if b.ExpiresAt == nil {
		return false
	}
	return b.ExpiresAt.Before(time.Now())
}

func (b *BetaKey) IsValid() bool {
	return b.IsActive && b.RevokedAt == nil && !b.IsExpired()
}

func (b *BetaKey) Sanitize() BetaKey {
	sanitized := *b
	sanitized.KeyHash = ""
	return sanitized
}
