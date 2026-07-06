package models

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var betaServiceTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}[a-z0-9]$`)

type BetaService struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Tag         string             `bson:"tag" json:"tag"`
	ServiceName string             `bson:"serviceName" json:"serviceName"`
	Label       string             `bson:"label" json:"label"`
	APIURL      string             `bson:"apiUrl" json:"apiUrl"`
	IsActive    bool               `bson:"isActive" json:"isActive"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
}

type CreateBetaServiceRequest struct {
	Tag         string `json:"tag"`
	ServiceName string `json:"serviceName"`
	Label       string `json:"label"`
	APIURL      string `json:"apiUrl"`
	IsActive    *bool  `json:"isActive,omitempty"`
}

type UpdateBetaServiceRequest struct {
	Label    *string `json:"label,omitempty"`
	APIURL   *string `json:"apiUrl,omitempty"`
	IsActive *bool   `json:"isActive,omitempty"`
}

type BetaServiceListResponse struct {
	Message  string        `json:"message"`
	Services []BetaService `json:"services"`
	Total    int           `json:"total"`
}

func normalizeBetaServiceTag(tag string) string {
	return strings.TrimSpace(strings.ToLower(tag))
}

func IsValidBetaServiceTag(tag string) bool {
	tag = normalizeBetaServiceTag(tag)
	if tag == "" || len(tag) < 3 || len(tag) > 50 {
		return false
	}
	return betaServiceTagPattern.MatchString(tag)
}

func (r *CreateBetaServiceRequest) Validate() error {
	r.Tag = normalizeBetaServiceTag(r.Tag)
	r.ServiceName = strings.TrimSpace(r.ServiceName)
	r.Label = strings.TrimSpace(r.Label)
	r.APIURL = strings.TrimSpace(r.APIURL)

	if !IsValidBetaServiceTag(r.Tag) {
		return errors.New("tag must be 3-50 lowercase alphanumeric characters with optional hyphens")
	}
	if r.ServiceName == "" {
		return errors.New("serviceName is required")
	}
	if !IsBetaServiceSupported(r.ServiceName) {
		return errors.New("beta is not supported for this service")
	}
	if r.Label == "" {
		return errors.New("label is required")
	}
	if len(r.Label) > 100 {
		return errors.New("label must be 100 characters or less")
	}
	if r.APIURL == "" {
		return errors.New("apiUrl is required")
	}
	if !strings.HasPrefix(r.APIURL, "http://") && !strings.HasPrefix(r.APIURL, "https://") {
		return errors.New("apiUrl must be an absolute http or https URL")
	}
	return nil
}

func (r *UpdateBetaServiceRequest) Validate() error {
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
	if r.APIURL != nil {
		trimmed := strings.TrimSpace(*r.APIURL)
		if trimmed == "" {
			return errors.New("apiUrl cannot be empty")
		}
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			return errors.New("apiUrl must be an absolute http or https URL")
		}
		r.APIURL = &trimmed
	}
	if r.Label == nil && r.APIURL == nil && r.IsActive == nil {
		return errors.New("at least one field must be provided")
	}
	return nil
}
