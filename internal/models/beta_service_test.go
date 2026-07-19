package models

import "testing"

func TestIsValidBetaServiceTag(t *testing.T) {
	tests := []struct {
		tag   string
		valid bool
	}{
		{"vlm-beta", true},
		{"cpu-v1", true},
		{"ab", false},
		{"bad_tag", false},
		{"has space", false},
		{"-bad", false},
		{"good-tag", true},
	}

	for _, tt := range tests {
		if got := IsValidBetaServiceTag(tt.tag); got != tt.valid {
			t.Fatalf("IsValidBetaServiceTag(%q) = %v, want %v", tt.tag, got, tt.valid)
		}
	}
}

func TestGenerateBetaKeyRequestRequiresBetaServiceTag(t *testing.T) {
	req := &GenerateBetaKeyRequest{
		ServiceName:       "signature-verification",
		Label:             "test",
		AssignedUserEmail: "user@example.com",
	}

	if err := req.Validate(); err == nil {
		t.Fatal("expected validation error when betaServiceTag is missing")
	}

	req.BetaServiceTag = "vlm-beta"
	if err := req.Validate(); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
}

func TestCreateBetaServiceRequestValidate(t *testing.T) {
	req := &CreateBetaServiceRequest{
		Tag:         "cpu-v1",
		ServiceName: "signature-verification",
		Label:       "CPU v1",
		APIURL:      "http://127.0.0.1:5008/sign_verify",
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
	if req.Tag != "cpu-v1" {
		t.Fatalf("expected normalized tag cpu-v1, got %q", req.Tag)
	}
}

func TestCreateBetaServiceRequestAllowsGenericServicePolicies(t *testing.T) {
	maxUpload := 25
	creditsPerHit := 7
	preferRegistryPull := true
	loginRequired := true
	req := &CreateBetaServiceRequest{
		Tag:         "ocr",
		ServiceName: "ocr",
		Label:       "OCR",
		APIURL:      "https://api.example.com/ocr",
		ServicePolicy: &ServicePolicy{
			Limits: &ServicePolicyLimits{
				MaxUploadSizeMB: &maxUpload,
				AllowedFormats:  []string{"PDF", " png "},
			},
			Pricing: &ServicePolicyPricing{
				CreditsPerHit: &creditsPerHit,
			},
			Notes: "  OCR policy  ",
		},
		RegistrySettings: &BetaServiceRegistrySettings{
			Provider:           "GHCR",
			Auth:               "TOKEN",
			Server:             "GHCR.IO",
			Namespace:          "/Automica-AI/",
			ImageTag:           "dev-2026",
			PreferRegistryPull: &preferRegistryPull,
			LoginRequired:      &loginRequired,
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected generic policy request to validate, got %v", err)
	}
	if got := req.ServicePolicy.Notes; got != "OCR policy" {
		t.Fatalf("expected notes trimmed, got %q", got)
	}
	if got := req.ServicePolicy.Limits.AllowedFormats; len(got) != 2 || got[0] != "pdf" || got[1] != "png" {
		t.Fatalf("expected normalized formats, got %#v", got)
	}
	if req.RegistrySettings == nil || req.RegistrySettings.Provider != "ghcr" || req.RegistrySettings.Namespace != "automica-ai" {
		t.Fatalf("expected normalized registry settings, got %+v", req.RegistrySettings)
	}
}

func TestUpdateBetaServiceRequestAllowsPolicyOnlyUpdate(t *testing.T) {
	creditsPerPage := 2
	req := &UpdateBetaServiceRequest{
		ServicePolicy: &ServicePolicy{
			Pricing: &ServicePolicyPricing{
				CreditsPerPage: &creditsPerPage,
			},
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected policy-only update to validate, got %v", err)
	}
}

func TestCreateBetaServiceRequestRejectsInvalidRegistrySettings(t *testing.T) {
	preferRegistryPull := true
	req := &CreateBetaServiceRequest{
		Tag:         "ocr-gpu",
		ServiceName: "ocr",
		Label:       "OCR GPU",
		APIURL:      "https://api.example.com/ocr",
		RegistrySettings: &BetaServiceRegistrySettings{
			Provider:           "ghcr",
			Server:             "https://ghcr.io",
			Namespace:          "automica-ai",
			ImageTag:           "dev",
			PreferRegistryPull: &preferRegistryPull,
		},
	}

	if err := req.Validate(); err == nil {
		t.Fatal("expected validation error for invalid registry server")
	}
}

func TestUpdateBetaServiceRequestAllowsRegistryOnlyUpdate(t *testing.T) {
	preferRegistryPull := true
	req := &UpdateBetaServiceRequest{
		RegistrySettings: &BetaServiceRegistrySettings{
			Provider:           "custom",
			Server:             "registry.example.com:5000",
			Namespace:          "team/backend",
			ImageTag:           "release-42",
			PreferRegistryPull: &preferRegistryPull,
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected registry-only update to validate, got %v", err)
	}
}

func TestRegistrySettingsDeriveRegionFromECRServer(t *testing.T) {
	settings := &BetaServiceRegistrySettings{
		Provider: "ecr",
		Server:   "123456789012.dkr.ecr.ap-south-1.amazonaws.com",
	}

	if err := settings.Validate(); err != nil {
		t.Fatalf("expected ecr settings to validate after deriving region, got %v", err)
	}
	if settings.Region != "ap-south-1" {
		t.Fatalf("expected region ap-south-1 from server, got %q", settings.Region)
	}
}

func TestRegistrySettingsApplyAutomicaECRDefaults(t *testing.T) {
	settings := &BetaServiceRegistrySettings{Provider: "ecr"}
	if err := settings.Validate(); err != nil {
		t.Fatalf("expected bare ecr provider to validate with Automica defaults, got %v", err)
	}
	if settings.Region != AutomicaECRRegion {
		t.Fatalf("expected region %s, got %q", AutomicaECRRegion, settings.Region)
	}
	if settings.Server == "" || settings.Namespace != AutomicaRegistryNamespace || settings.Auth != "aws" {
		t.Fatalf("expected Automica ECR defaults, got %+v", settings)
	}
}
