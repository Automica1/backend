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
