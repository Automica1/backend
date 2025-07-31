// internal/models/api_key.go
package models

import (
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type APIKey struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	UserID      string             `bson:"userId" json:"userId"`
	Email       string             `bson:"email" json:"email"`
	KeyName     string             `bson:"keyName" json:"keyName"`
	KeyHash     string             `bson:"keyHash" json:"-"` // Never expose in JSON
	KeyPrefix   string             `bson:"keyPrefix" json:"keyPrefix"` // First 8 chars for identification
	IsActive    bool               `bson:"isActive" json:"isActive"`
	LastUsedAt  *time.Time         `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	UsageCount  int64              `bson:"usageCount" json:"usageCount"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
	ExpiresAt   *time.Time         `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}

type CreateAPIKeyRequest struct {
	KeyName   string     `json:"keyName" validate:"required,min=1,max=50"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type CreateAPIKeyResponse struct {
	Message   string     `json:"message"`
	APIKey    string     `json:"apiKey"` // Full key returned only once
	KeyName   string     `json:"keyName"`
	KeyPrefix string     `json:"keyPrefix"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// APIKeyResponse for getting a single API key (replaces APIKeyListResponse)
type APIKeyResponse struct {
	Message string  `json:"message"`
	Key     *APIKey `json:"key,omitempty"` // Single key instead of array
}

// Keep APIKeyListResponse for backward compatibility but mark as deprecated
// Deprecated: Use APIKeyResponse instead
type APIKeyListResponse struct {
	Message string   `json:"message"`
	Keys    []APIKey `json:"keys"`
	Total   int      `json:"total"`
}

type UpdateAPIKeyRequest struct {
	KeyName  string `json:"keyName,omitempty"`
	IsActive *bool  `json:"isActive,omitempty"`
}

// Removed RevokeAPIKeyRequest since we no longer need keyID

// APIKeyStats represents aggregate statistics for user's API key (now singular)
type APIKeyStats struct {
	TotalKeys    int        `json:"totalKeys"`    // Will always be 0 or 1
	ActiveKeys   int        `json:"activeKeys"`   // Will always be 0 or 1
	InactiveKeys int        `json:"inactiveKeys"` // Will always be 0 or 1
	ExpiredKeys  int        `json:"expiredKeys"`  // Will always be 0 or 1
	TotalUsage   int64      `json:"totalUsage"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	OldestKeyAt  *time.Time `json:"oldestKeyAt,omitempty"`  // Same as CreatedAt for single key
	NewestKeyAt  *time.Time `json:"newestKeyAt,omitempty"`  // Same as CreatedAt for single key
}

// APIKeyStatsResponse represents the response for API key statistics
type APIKeyStatsResponse struct {
	Message string      `json:"message"`
	Stats   APIKeyStats `json:"stats"`
}

// IndividualAPIKeyStats represents statistics for a single API key
type IndividualAPIKeyStats struct {
	KeyID           primitive.ObjectID `json:"keyId"`
	KeyName         string             `json:"keyName"`
	KeyPrefix       string             `json:"keyPrefix"`
	TotalRequests   int64              `json:"totalRequests"`
	LastUsed        *time.Time         `json:"lastUsed,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	IsActive        bool               `json:"isActive"`
	ExpiresAt       *time.Time         `json:"expiresAt,omitempty"`
	DaysUntilExpiry *int               `json:"daysUntilExpiry,omitempty"`
}

// Validation methods
func (r *CreateAPIKeyRequest) Validate() error {
	if strings.TrimSpace(r.KeyName) == "" {
		return errors.New("keyName is required")
	}
	
	r.KeyName = strings.TrimSpace(r.KeyName)
	
	if len(r.KeyName) > 50 {
		return errors.New("keyName must be 50 characters or less")
	}
	
	if len(r.KeyName) < 1 {
		return errors.New("keyName must be at least 1 character long")
	}
	
	if r.ExpiresAt != nil && r.ExpiresAt.Before(time.Now()) {
		return errors.New("expiresAt must be in the future")
	}
	
	// Validate expiry date is not too far in the future (e.g., max 1 year)
	if r.ExpiresAt != nil && r.ExpiresAt.After(time.Now().AddDate(1, 0, 0)) {
		return errors.New("expiresAt cannot be more than 1 year in the future")
	}
	
	return nil
}

func (r *UpdateAPIKeyRequest) Validate() error {
	if r.KeyName != "" {
		r.KeyName = strings.TrimSpace(r.KeyName)
		
		if len(r.KeyName) > 50 {
			return errors.New("keyName must be 50 characters or less")
		}
		
		if len(r.KeyName) < 1 {
			return errors.New("keyName must be at least 1 character long")
		}
	}
	
	// Validate that at least one field is being updated
	if r.KeyName == "" && r.IsActive == nil {
		return errors.New("at least one field must be provided for update")
	}
	
	return nil
}

// Helper methods for APIKey
func (a *APIKey) IsExpired() bool {
	if a.ExpiresAt == nil {
		return false
	}
	return a.ExpiresAt.Before(time.Now())
}

func (a *APIKey) IsValid() bool {
	return a.IsActive && !a.IsExpired()
}

func (a *APIKey) DaysUntilExpiry() *int {
	if a.ExpiresAt == nil {
		return nil
	}
	
	days := int(time.Until(*a.ExpiresAt).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return &days
}

// ToIndividualStats converts APIKey to IndividualAPIKeyStats
func (a *APIKey) ToIndividualStats() IndividualAPIKeyStats {
	return IndividualAPIKeyStats{
		KeyID:           a.ID,
		KeyName:         a.KeyName,
		KeyPrefix:       a.KeyPrefix,
		TotalRequests:   a.UsageCount,
		LastUsed:        a.LastUsedAt,
		CreatedAt:       a.CreatedAt,
		IsActive:        a.IsActive,
		ExpiresAt:       a.ExpiresAt,
		DaysUntilExpiry: a.DaysUntilExpiry(),
	}
}

// Sanitize removes sensitive information from APIKey for public responses
func (a *APIKey) Sanitize() APIKey {
	sanitized := *a
	sanitized.KeyHash = "" // Ensure hash is never exposed
	return sanitized
}

// APIKeyValidationRequest for validating API keys during authentication
type APIKeyValidationRequest struct {
	APIKey string `json:"apiKey" validate:"required"`
}

func (r *APIKeyValidationRequest) Validate() error {
	if strings.TrimSpace(r.APIKey) == "" {
		return errors.New("apiKey is required")
	}
	
	// Validate API key format (should start with ak_live_)
	if !strings.HasPrefix(r.APIKey, "ak_live_") {
		return errors.New("invalid API key format")
	}
	
	// Validate length (ak_live_ + 64 hex characters = 72 total)
	if len(r.APIKey) != 72 {
		return errors.New("invalid API key length")
	}
	
	return nil
}