package models

import (
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GuestPassServiceSlugs are frontend/Try API service identifiers.
var GuestPassServiceSlugs = []string{
	"signature-verification",
	"qr-extract",
	"qr-mask",
	"id-crop",
	"document-enhance",
	"face-verify",
	"face-cropping",
}

var guestPassServiceSet = func() map[string]bool {
	m := make(map[string]bool, len(GuestPassServiceSlugs))
	for _, slug := range GuestPassServiceSlugs {
		m[slug] = true
	}
	return m
}()

func IsGuestPassServiceSupported(serviceSlug string) bool {
	return guestPassServiceSet[serviceSlug]
}

type GuestPass struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	KeyHash          string             `bson:"keyHash" json:"-"`
	KeyPrefix        string             `bson:"keyPrefix" json:"keyPrefix"`
	WalletUserID     string             `bson:"walletUserId" json:"walletUserId"`
	Label            string             `bson:"label" json:"label"`
	Description      string             `bson:"description,omitempty" json:"description,omitempty"`
	InitialCredits   int                `bson:"initialCredits" json:"initialCredits"`
	RemainingCredits int                `bson:"remainingCredits" json:"remainingCredits"`
	AllowedServices  []string           `bson:"allowedServices" json:"allowedServices"`
	CreatedBy        string             `bson:"createdBy" json:"createdBy"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
	ExpiresAt        *time.Time         `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
	RevokedAt        *time.Time         `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
	IsActive         bool               `bson:"isActive" json:"isActive"`
	UsageCount       int64              `bson:"usageCount" json:"usageCount"`
	LastUsedAt       *time.Time         `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
}

func (g *GuestPass) AllowsService(serviceSlug string) bool {
	if len(g.AllowedServices) == 0 {
		return true
	}
	for _, allowed := range g.AllowedServices {
		if allowed == serviceSlug {
			return true
		}
	}
	return false
}

func (g *GuestPass) IsExpired() bool {
	if g.ExpiresAt == nil {
		return false
	}
	return g.ExpiresAt.Before(time.Now())
}

func (g *GuestPass) IsValid() bool {
	return g.IsActive && g.RevokedAt == nil && !g.IsExpired()
}

func (g *GuestPass) Sanitize() GuestPass {
	sanitized := *g
	sanitized.KeyHash = ""
	return sanitized
}

type CreateGuestPassRequest struct {
	Label           string   `json:"label"`
	Description     string   `json:"description,omitempty"`
	Credits         int      `json:"credits"`
	AllowedServices []string `json:"allowedServices,omitempty"`
	ExpiresInDays   *int     `json:"expiresInDays,omitempty"`
}

type CreateGuestPassResponse struct {
	Message          string     `json:"message"`
	GuestPassKey     string     `json:"guestPassKey"`
	KeyPrefix        string     `json:"keyPrefix"`
	Label            string     `json:"label"`
	InitialCredits   int        `json:"initialCredits"`
	AllowedServices  []string   `json:"allowedServices"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type UpdateGuestPassRequest struct {
	Label           *string  `json:"label,omitempty"`
	Description     *string  `json:"description,omitempty"`
	AllowedServices []string `json:"allowedServices,omitempty"`
	TopUpCredits    *int     `json:"topUpCredits,omitempty"`
	ExpiresInDays   *int     `json:"expiresInDays,omitempty"`
}

type GuestPassListResponse struct {
	Message string      `json:"message"`
	Passes  []GuestPass `json:"passes"`
	Total   int         `json:"total"`
}

type GuestPassBalanceResponse struct {
	Message          string     `json:"message"`
	Label            string     `json:"label"`
	RemainingCredits int        `json:"remainingCredits"`
	AllowedServices  []string   `json:"allowedServices"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	ServiceAllowed   bool       `json:"serviceAllowed,omitempty"`
}

type GuestPassValidateResponse struct {
	Message          string   `json:"message"`
	Valid            bool     `json:"valid"`
	RemainingCredits int      `json:"remainingCredits,omitempty"`
	AllowedServices  []string `json:"allowedServices,omitempty"`
	ServiceAllowed   bool     `json:"serviceAllowed,omitempty"`
}

type RevokeGuestPassResponse struct {
	Message string `json:"message"`
	ID      string `json:"id"`
}

func (r *CreateGuestPassRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	r.Description = strings.TrimSpace(r.Description)
	if r.Label == "" {
		return errors.New("label is required")
	}
	if len(r.Label) > 100 {
		return errors.New("label must be 100 characters or less")
	}
	if r.Credits <= 0 {
		return errors.New("credits must be positive")
	}
	if r.Credits > 100000 {
		return errors.New("credits must be 100000 or less")
	}
	for _, svc := range r.AllowedServices {
		if !IsGuestPassServiceSupported(svc) {
			return errors.New("unsupported service: " + svc)
		}
	}
	if r.ExpiresInDays != nil {
		if *r.ExpiresInDays < 1 || *r.ExpiresInDays > 365 {
			return errors.New("expiresInDays must be between 1 and 365")
		}
	}
	return nil
}

func (r *UpdateGuestPassRequest) Validate() error {
	if r.Label != nil {
		trimmed := strings.TrimSpace(*r.Label)
		if trimmed == "" {
			return errors.New("label cannot be empty")
		}
		if len(trimmed) > 100 {
			return errors.New("label must be 100 characters or less")
		}
		r.Label = &trimmed
	}
	if r.Description != nil {
		trimmed := strings.TrimSpace(*r.Description)
		r.Description = &trimmed
	}
	for _, svc := range r.AllowedServices {
		if !IsGuestPassServiceSupported(svc) {
			return errors.New("unsupported service: " + svc)
		}
	}
	if r.TopUpCredits != nil && *r.TopUpCredits <= 0 {
		return errors.New("topUpCredits must be positive")
	}
	if r.ExpiresInDays != nil {
		if *r.ExpiresInDays < 1 || *r.ExpiresInDays > 365 {
			return errors.New("expiresInDays must be between 1 and 365")
		}
	}
	return nil
}

// NormalizeGuestPassKey normalizes user input for lookup.
func NormalizeGuestPassKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, " ", "-")
	key = strings.ReplaceAll(key, "_", "-")
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	return strings.Trim(key, "-")
}
