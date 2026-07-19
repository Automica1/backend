package services

import (
	"context"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
)

type betaServiceRepoStub struct {
	service *models.BetaService
}

func (s *betaServiceRepoStub) Create(ctx context.Context, service *models.BetaService) error {
	s.service = service
	return nil
}

func (s *betaServiceRepoStub) GetByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	if s.service != nil && s.service.Tag == tag {
		return s.service, nil
	}
	return nil, nil
}

func (s *betaServiceRepoStub) GetActiveByServiceName(ctx context.Context, serviceName string) (*models.BetaService, error) {
	if s.service != nil && s.service.ServiceName == serviceName && s.service.IsActive {
		return s.service, nil
	}
	return nil, nil
}

func (s *betaServiceRepoStub) GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	if s.service != nil && s.service.Tag == tag && s.service.IsActive {
		return s.service, nil
	}
	return nil, nil
}

func (s *betaServiceRepoStub) List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error) {
	if s.service == nil {
		return nil, nil
	}
	return []*models.BetaService{s.service}, nil
}

func (s *betaServiceRepoStub) Update(ctx context.Context, tag string, update bson.M) (*models.BetaService, error) {
	if s.service == nil || s.service.Tag != tag {
		return nil, nil
	}
	if v, ok := update["servicePolicy"]; ok {
		s.service.ServicePolicy = v.(*models.ServicePolicy)
	}
	if v, ok := update["label"]; ok {
		s.service.Label = v.(string)
	}
	if v, ok := update["apiUrl"]; ok {
		s.service.APIURL = v.(string)
	}
	if v, ok := update["isActive"]; ok {
		s.service.IsActive = v.(bool)
	}
	if v, ok := update["registrySettings"]; ok {
		s.service.RegistrySettings = v.(*models.BetaServiceRegistrySettings)
	}
	s.service.UpdatedAt = time.Now()
	return s.service, nil
}

func TestBetaServiceServiceCarriesPolicyAndSupportsGenericLookup(t *testing.T) {
	maxUpload := 12
	hitCredits := 5
	preferRegistryPull := true
	loginRequired := true
	repo := &betaServiceRepoStub{}
	svc := NewBetaServiceService(repo)

	created, err := svc.Create(context.Background(), &models.CreateBetaServiceRequest{
		Tag:         "ocr",
		ServiceName: "ocr",
		Label:       "OCR",
		APIURL:      "https://api.example.com/ocr",
		ServicePolicy: &models.ServicePolicy{
			Limits:  &models.ServicePolicyLimits{MaxUploadSizeMB: &maxUpload},
			Pricing: &models.ServicePolicyPricing{CreditsPerHit: &hitCredits},
			Notes:   "ocr policy",
		},
		RegistrySettings: &models.BetaServiceRegistrySettings{
			Provider:           "ghcr",
			Auth:               "token",
			Server:             "ghcr.io",
			Namespace:          "automica-ai",
			ImageTag:           "dev",
			PreferRegistryPull: &preferRegistryPull,
			LoginRequired:      &loginRequired,
		},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.ServicePolicy == nil || created.ServicePolicy.Pricing == nil || created.ServicePolicy.Pricing.CreditsPerHit == nil {
		t.Fatalf("expected policy to be attached, got %+v", created)
	}
	if created.RegistrySettings == nil || created.RegistrySettings.Server != "ghcr.io" {
		t.Fatalf("expected registry settings to be attached, got %+v", created.RegistrySettings)
	}

	got, err := svc.GetActiveByServiceName(context.Background(), "ocr")
	if err != nil {
		t.Fatalf("GetActiveByServiceName returned error: %v", err)
	}
	if got == nil || got.Tag != "ocr" {
		t.Fatalf("unexpected service lookup result: %+v", got)
	}
}

func TestBetaServiceServiceUpdateCarriesRegistrySettings(t *testing.T) {
	preferRegistryPull := true
	loginRequired := true
	repo := &betaServiceRepoStub{
		service: &models.BetaService{
			Tag:         "ocr-gpu",
			ServiceName: "ocr",
			Label:       "OCR GPU",
			APIURL:      "https://api.example.com/ocr",
			IsActive:    true,
		},
	}
	svc := NewBetaServiceService(repo)

	updated, err := svc.Update(context.Background(), "ocr-gpu", &models.UpdateBetaServiceRequest{
		RegistrySettings: &models.BetaServiceRegistrySettings{
			Provider:           "ghcr",
			Auth:               "token",
			Server:             "ghcr.io",
			Namespace:          "automica-ai",
			ImageTag:           "2026-07-16",
			PreferRegistryPull: &preferRegistryPull,
			LoginRequired:      &loginRequired,
		},
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.RegistrySettings == nil || updated.RegistrySettings.ImageTag != "2026-07-16" {
		t.Fatalf("expected updated registry settings, got %+v", updated.RegistrySettings)
	}
}
