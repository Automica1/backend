package models

import (
	"errors"
	"fmt"
	"strings"
)

type ServicePolicy struct {
	Limits  *ServicePolicyLimits  `bson:"limits,omitempty" json:"limits,omitempty"`
	Pricing *ServicePolicyPricing `bson:"pricing,omitempty" json:"pricing,omitempty"`
	Notes   string                `bson:"notes,omitempty" json:"notes,omitempty"`
}

type ServicePolicyLimits struct {
	MaxUploadSizeMB *int     `bson:"maxUploadSizeMB,omitempty" json:"maxUploadSizeMB,omitempty"`
	MaxPages        *int     `bson:"maxPages,omitempty" json:"maxPages,omitempty"`
	MaxFiles        *int     `bson:"maxFiles,omitempty" json:"maxFiles,omitempty"`
	AllowedFormats  []string `bson:"allowedFormats,omitempty" json:"allowedFormats,omitempty"`
}

type ServicePolicyPricing struct {
	Mode             string `bson:"mode,omitempty" json:"mode,omitempty"`
	CreditsPerHit    *int   `bson:"creditsPerHit,omitempty" json:"creditsPerHit,omitempty"`
	CreditsPerPage   *int   `bson:"creditsPerPage,omitempty" json:"creditsPerPage,omitempty"`
	StartupCredits   *int   `bson:"startupCredits,omitempty" json:"startupCredits,omitempty"`
	CreditsPerMinute *int   `bson:"creditsPerMinute,omitempty" json:"creditsPerMinute,omitempty"`
}

func (p *ServicePolicy) Normalize() {
	if p == nil {
		return
	}
	p.Notes = strings.TrimSpace(p.Notes)
	if p.Limits != nil {
		p.Limits.Normalize()
	}
	if p.Pricing != nil {
		p.Pricing.Normalize()
	}
}

func (p *ServicePolicy) Validate() error {
	if p == nil {
		return nil
	}
	p.Normalize()
	if p.Limits != nil {
		if err := p.Limits.Validate(); err != nil {
			return err
		}
	}
	if p.Pricing != nil {
		if err := p.Pricing.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (l *ServicePolicyLimits) Normalize() {
	if l == nil {
		return
	}
	formats := make([]string, 0, len(l.AllowedFormats))
	for _, format := range l.AllowedFormats {
		normalized := strings.ToLower(strings.TrimSpace(format))
		if normalized == "" {
			continue
		}
		formats = append(formats, normalized)
	}
	l.AllowedFormats = formats
}

func (l *ServicePolicyLimits) Validate() error {
	if l == nil {
		return nil
	}
	if l.MaxUploadSizeMB != nil && *l.MaxUploadSizeMB <= 0 {
		return errors.New("limits.maxUploadSizeMB must be greater than 0")
	}
	if l.MaxPages != nil && *l.MaxPages <= 0 {
		return errors.New("limits.maxPages must be greater than 0")
	}
	if l.MaxFiles != nil && *l.MaxFiles <= 0 {
		return errors.New("limits.maxFiles must be greater than 0")
	}
	return nil
}

func (p *ServicePolicyPricing) Normalize() {
	if p == nil {
		return
	}
	p.Mode = strings.TrimSpace(strings.ToLower(p.Mode))
}

func (p *ServicePolicyPricing) Validate() error {
	if p == nil {
		return nil
	}
	if p.CreditsPerHit != nil && *p.CreditsPerHit < 0 {
		return errors.New("pricing.creditsPerHit cannot be negative")
	}
	if p.CreditsPerPage != nil && *p.CreditsPerPage < 0 {
		return errors.New("pricing.creditsPerPage cannot be negative")
	}
	if p.StartupCredits != nil && *p.StartupCredits < 0 {
		return errors.New("pricing.startupCredits cannot be negative")
	}
	if p.CreditsPerMinute != nil && *p.CreditsPerMinute < 0 {
		return errors.New("pricing.creditsPerMinute cannot be negative")
	}
	return nil
}

func (p *ServicePolicy) HitCredits(defaultValue int) int {
	if p != nil && p.Pricing != nil && p.Pricing.CreditsPerHit != nil {
		return *p.Pricing.CreditsPerHit
	}
	return defaultValue
}

func (p *ServicePolicy) PageCredits(defaultValue int) int {
	if p != nil && p.Pricing != nil && p.Pricing.CreditsPerPage != nil {
		return *p.Pricing.CreditsPerPage
	}
	return defaultValue
}

func (p *ServicePolicy) Summary() string {
	if p == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if p.Limits != nil {
		if p.Limits.MaxUploadSizeMB != nil {
			parts = append(parts, fmt.Sprintf("max upload %dMB", *p.Limits.MaxUploadSizeMB))
		}
		if p.Limits.MaxPages != nil {
			parts = append(parts, fmt.Sprintf("max pages %d", *p.Limits.MaxPages))
		}
		if len(p.Limits.AllowedFormats) > 0 {
			parts = append(parts, fmt.Sprintf("formats %s", strings.Join(p.Limits.AllowedFormats, ", ")))
		}
	}
	if p.Pricing != nil {
		if p.Pricing.CreditsPerHit != nil {
			parts = append(parts, fmt.Sprintf("%d credits/hit", *p.Pricing.CreditsPerHit))
		}
		if p.Pricing.CreditsPerPage != nil {
			parts = append(parts, fmt.Sprintf("%d credits/page", *p.Pricing.CreditsPerPage))
		}
		if p.Pricing.StartupCredits != nil || p.Pricing.CreditsPerMinute != nil {
			startup := 0
			perMinute := 0
			if p.Pricing.StartupCredits != nil {
				startup = *p.Pricing.StartupCredits
			}
			if p.Pricing.CreditsPerMinute != nil {
				perMinute = *p.Pricing.CreditsPerMinute
			}
			parts = append(parts, fmt.Sprintf("session %d start + %d/min", startup, perMinute))
		}
	}
	if p.Notes != "" {
		parts = append(parts, p.Notes)
	}
	return strings.Join(parts, " · ")
}
