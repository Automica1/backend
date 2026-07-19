package models

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var betaServiceTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}[a-z0-9]$`)
var betaServiceRegistryServerPattern = regexp.MustCompile(`^(localhost|[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)(?::[0-9]{2,5})?$`)
var betaServiceRegistryNamespacePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
var betaServiceRegistryRegionPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
var betaServiceRegistryImageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
var betaServiceECRServerRegionPattern = regexp.MustCompile(`^[0-9]+\.dkr\.ecr\.([^.]+)\.amazonaws\.com(?:\.cn)?$`)

// Automica script-aligned registry defaults (see scripts/push_ocr_dev2_ecr.sh,
// scripts/gpu_host_registry_env.sh). Admin provider switches should not require
// retyping these; ApplyAutomicaDefaults fills blanks the same way scripts do.
const (
	AutomicaRegistryNamespace = "automica-ai"
	AutomicaGHCRServer        = "ghcr.io"
	AutomicaECRAccountID      = "472929790350"
	AutomicaECRRegion         = "eu-north-1"
)

type BetaService struct {
	ID               primitive.ObjectID           `bson:"_id,omitempty" json:"id,omitempty"`
	Tag              string                       `bson:"tag" json:"tag"`
	ServiceName      string                       `bson:"serviceName" json:"serviceName"`
	Label            string                       `bson:"label" json:"label"`
	APIURL           string                       `bson:"apiUrl" json:"apiUrl"`
	IsActive         bool                         `bson:"isActive" json:"isActive"`
	ServicePolicy    *ServicePolicy               `bson:"servicePolicy,omitempty" json:"servicePolicy,omitempty"`
	RegistrySettings *BetaServiceRegistrySettings `bson:"registrySettings,omitempty" json:"registrySettings,omitempty"`
	CreatedAt        time.Time                    `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time                    `bson:"updatedAt" json:"updatedAt"`
}

type BetaServiceRegistrySettings struct {
	Provider           string `bson:"provider,omitempty" json:"provider,omitempty"`
	Auth               string `bson:"auth,omitempty" json:"auth,omitempty"`
	Server             string `bson:"server,omitempty" json:"server,omitempty"`
	Namespace          string `bson:"namespace,omitempty" json:"namespace,omitempty"`
	Region             string `bson:"region,omitempty" json:"region,omitempty"`
	ImageTag           string `bson:"imageTag,omitempty" json:"imageTag,omitempty"`
	PreferRegistryPull *bool  `bson:"preferRegistryPull,omitempty" json:"preferRegistryPull,omitempty"`
	LoginRequired      *bool  `bson:"loginRequired,omitempty" json:"loginRequired,omitempty"`
}

type CreateBetaServiceRequest struct {
	Tag              string                       `json:"tag"`
	ServiceName      string                       `json:"serviceName"`
	Label            string                       `json:"label"`
	APIURL           string                       `json:"apiUrl"`
	IsActive         *bool                        `json:"isActive,omitempty"`
	ServicePolicy    *ServicePolicy               `json:"servicePolicy,omitempty"`
	RegistrySettings *BetaServiceRegistrySettings `json:"registrySettings,omitempty"`
}

type UpdateBetaServiceRequest struct {
	Label            *string                      `json:"label,omitempty"`
	APIURL           *string                      `json:"apiUrl,omitempty"`
	IsActive         *bool                        `json:"isActive,omitempty"`
	ServicePolicy    *ServicePolicy               `json:"servicePolicy,omitempty"`
	RegistrySettings *BetaServiceRegistrySettings `json:"registrySettings,omitempty"`
}

type BetaServiceListResponse struct {
	Message  string        `json:"message"`
	Services []BetaService `json:"services"`
	Total    int           `json:"total"`
}

func normalizeBetaServiceTag(tag string) string {
	return strings.TrimSpace(strings.ToLower(tag))
}

func (s *BetaServiceRegistrySettings) Normalize() {
	if s == nil {
		return
	}
	s.Provider = strings.TrimSpace(strings.ToLower(s.Provider))
	s.Auth = strings.TrimSpace(strings.ToLower(s.Auth))
	s.Server = strings.TrimSpace(strings.ToLower(s.Server))
	s.Namespace = strings.Trim(strings.TrimSpace(strings.ToLower(s.Namespace)), "/")
	s.Region = strings.TrimSpace(strings.ToLower(s.Region))
	s.ImageTag = strings.TrimSpace(s.ImageTag)
}

// ApplyAutomicaDefaults fills blank GHCR/ECR fields with the same fixed values
// scripts already use. Explicit overrides always win.
func (s *BetaServiceRegistrySettings) ApplyAutomicaDefaults() {
	if s == nil {
		return
	}
	s.Normalize()
	if s.Region == "" && s.Server != "" {
		if match := betaServiceECRServerRegionPattern.FindStringSubmatch(s.Server); len(match) == 2 {
			s.Region = match[1]
		}
	}
	switch s.Provider {
	case "ghcr":
		if s.Auth == "" {
			s.Auth = "token"
		}
		if s.Server == "" {
			s.Server = AutomicaGHCRServer
		}
		if s.Namespace == "" {
			s.Namespace = AutomicaRegistryNamespace
		}
	case "ecr":
		if s.Auth == "" || s.Auth == "none" {
			s.Auth = "aws"
		}
		if s.Region == "" {
			s.Region = AutomicaECRRegion
		}
		if s.Server == "" {
			s.Server = AutomicaECRAccountID + ".dkr.ecr." + s.Region + ".amazonaws.com"
		}
		if s.Namespace == "" {
			s.Namespace = AutomicaRegistryNamespace
		}
	}
}

func (s *BetaServiceRegistrySettings) IsEmpty() bool {
	if s == nil {
		return true
	}
	return s.Provider == "" &&
		s.Auth == "" &&
		s.Server == "" &&
		s.Namespace == "" &&
		s.Region == "" &&
		s.ImageTag == "" &&
		s.PreferRegistryPull == nil &&
		s.LoginRequired == nil
}

func (s *BetaServiceRegistrySettings) Validate() error {
	if s == nil {
		return nil
	}
	s.Normalize()
	if s.IsEmpty() {
		return nil
	}

	switch s.Provider {
	case "ghcr", "ecr", "gcr", "gar", "dockerhub", "acr", "quay", "custom":
	case "":
		return errors.New("registrySettings.provider is required when registrySettings is provided")
	default:
		return errors.New("registrySettings.provider must be one of ghcr, ecr, gcr, gar, dockerhub, acr, quay, or custom")
	}

	// Provider switch from admin only needs provider (+ optional image tag).
	// Fill Automica script defaults before the rest of validation.
	s.ApplyAutomicaDefaults()

	switch s.Auth {
	case "", "none", "basic", "token", "aws", "gcp", "azure":
	default:
		return errors.New("registrySettings.auth must be one of none, basic, token, aws, gcp, or azure")
	}

	if s.Server != "" && !betaServiceRegistryServerPattern.MatchString(s.Server) {
		return errors.New("registrySettings.server must be a registry host like ghcr.io or registry.example.com:5000")
	}
	if s.Namespace != "" && !betaServiceRegistryNamespacePattern.MatchString(s.Namespace) {
		return errors.New("registrySettings.namespace must be lowercase registry path segments separated by slashes")
	}
	if s.Region != "" && !betaServiceRegistryRegionPattern.MatchString(s.Region) {
		return errors.New("registrySettings.region must contain only lowercase letters, numbers, and hyphens")
	}
	if s.ImageTag != "" && !betaServiceRegistryImageTagPattern.MatchString(s.ImageTag) {
		return errors.New("registrySettings.imageTag must be a valid container image tag")
	}
	if s.Server != "" && strings.Contains(s.Server, "://") {
		return errors.New("registrySettings.server must not include a URL scheme")
	}
	if s.Namespace != "" && strings.Contains(s.Namespace, ":") {
		return errors.New("registrySettings.namespace must not include a tag or port")
	}
	if s.Provider == "ecr" && s.Region == "" {
		return errors.New("registrySettings.region is required when provider is ecr")
	}
	if s.LoginRequired != nil && *s.LoginRequired {
		if s.Auth == "" || s.Auth == "none" {
			return errors.New("registrySettings.auth must be set when loginRequired is true")
		}
	}
	if s.PreferRegistryPull != nil && *s.PreferRegistryPull {
		if s.Server == "" {
			return errors.New("registrySettings.server is required when preferRegistryPull is true")
		}
		if s.Namespace == "" {
			return errors.New("registrySettings.namespace is required when preferRegistryPull is true")
		}
		if s.ImageTag == "" {
			return errors.New("registrySettings.imageTag is required when preferRegistryPull is true")
		}
	}
	return nil
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
	if r.ServicePolicy != nil {
		if err := r.ServicePolicy.Validate(); err != nil {
			return err
		}
	}
	if r.RegistrySettings != nil {
		if err := r.RegistrySettings.Validate(); err != nil {
			return err
		}
		if r.RegistrySettings.IsEmpty() {
			r.RegistrySettings = nil
		}
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
	if r.ServicePolicy != nil {
		if err := r.ServicePolicy.Validate(); err != nil {
			return err
		}
	}
	if r.RegistrySettings != nil {
		if err := r.RegistrySettings.Validate(); err != nil {
			return err
		}
		if r.RegistrySettings.IsEmpty() {
			r.RegistrySettings = nil
		}
	}
	if r.Label == nil && r.APIURL == nil && r.IsActive == nil && r.ServicePolicy == nil && r.RegistrySettings == nil {
		return errors.New("at least one field must be provided")
	}
	return nil
}
