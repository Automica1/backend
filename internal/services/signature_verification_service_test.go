package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"chi-mongo-backend/internal/models"
)

func TestSignatureVerificationAPIServiceUsesBetaServiceURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sign_verify" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"req_id":  "req-1",
			"success": true,
			"status":  "completed",
			"data": map[string]interface{}{
				"similarity_percentage": 88.5,
				"classification":        "Genuine",
			},
		})
	}))
	defer server.Close()

	svc := &signatureVerificationAPIService{
		httpClient:     server.Client(),
		betaHttpClient: server.Client(),
		prodURL:        "http://prod.example/sign_verify",
	}

	result, err := svc.ProcessSignatureVerification(context.Background(), &models.SignatureVerificationRequest{
		ReqID:     "req-1",
		DocBase64: []string{"a", "b"},
	}, &SignatureVerificationProcessOptions{
		UseBeta:        true,
		BetaServiceURL: server.URL + "/sign_verify",
	})
	if err != nil {
		t.Fatalf("ProcessSignatureVerification returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful result, got %+v", result)
	}
}

func TestSignatureVerificationAPIServiceBetaRequiresURL(t *testing.T) {
	svc := &signatureVerificationAPIService{
		httpClient: http.DefaultClient,
		prodURL:    "http://prod.example/sign_verify",
	}

	_, err := svc.ProcessSignatureVerification(context.Background(), &models.SignatureVerificationRequest{
		ReqID:     "req-1",
		DocBase64: []string{"a", "b"},
	}, &SignatureVerificationProcessOptions{
		UseBeta: true,
	})
	if err == nil {
		t.Fatal("expected error when beta URL is missing")
	}
}
