// internal/services/signature_verification_service.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
)

type SignatureVerificationProcessOptions struct {
	UseBeta        bool
	BetaServiceURL string
}

type SignatureVerificationAPIService interface {
	ProcessSignatureVerification(ctx context.Context, req *models.SignatureVerificationRequest, opts *SignatureVerificationProcessOptions) (*models.SignatureVerificationResult, error)
}

type signatureVerificationAPIService struct {
	httpClient     *http.Client
	betaHttpClient *http.Client
	prodURL        string
	gatewayKey     string
}

func NewSignatureVerificationAPIService() SignatureVerificationAPIService {
	betaTimeout := 180 * time.Second
	if raw := strings.TrimSpace(os.Getenv("SIGNATURE_VERIFY_BETA_TIMEOUT_SECONDS")); raw != "" {
		if secs, err := time.ParseDuration(raw + "s"); err == nil && secs > 0 {
			betaTimeout = secs
		}
	}

	return &signatureVerificationAPIService{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		betaHttpClient: &http.Client{
			Timeout: betaTimeout,
		},
		prodURL:    getEnvOrDefault("VERIFY_SIGNATURE_API_URL"),
		gatewayKey: os.Getenv("BETA_ML_GATEWAY_KEY"),
	}
}

func (s *signatureVerificationAPIService) ProcessSignatureVerification(ctx context.Context, req *models.SignatureVerificationRequest, opts *SignatureVerificationProcessOptions) (*models.SignatureVerificationResult, error) {
	useBeta := opts != nil && opts.UseBeta
	apiURL := s.prodURL
	if useBeta {
		betaURL := ""
		if opts != nil {
			betaURL = strings.TrimSpace(opts.BetaServiceURL)
		}
		if betaURL == "" {
			return nil, fmt.Errorf("beta signature verification is not configured")
		}
		apiURL = betaURL
	}

	payload := map[string]interface{}{
		"req_id":     req.ReqID,
		"doc_base64": req.DocBase64,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if useBeta && s.gatewayKey != "" {
		httpReq.Header.Set("X-Internal-Gateway-Key", s.gatewayKey)
	}

	log.Printf("Making Signature Verification API request to: %s (beta=%t)", apiURL, useBeta)
	log.Printf("Signature Verification request ReqID=%s payload_bytes=%d", req.ReqID, len(jsonData))

	client := s.httpClient
	if useBeta {
		client = s.betaHttpClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call signature verification API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("Signature Verification API response status: %d", resp.StatusCode)
	log.Printf("Signature Verification API response bytes: %d", len(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signature verification API returned non-OK status %d: %s", resp.StatusCode, truncateForLog(string(body), 500))
	}

	var apiResponse struct {
		ReqID        string  `json:"req_id"`
		Success      bool    `json:"success"`
		Status       string  `json:"status"`
		Message      string  `json:"message"`
		ErrorMessage *string `json:"error_message"`
		Data         *struct {
			SimilarityPercentage float64 `json:"similarity_percentage"`
			Classification       string  `json:"classification"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	result := &models.SignatureVerificationResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
	}

	if apiResponse.Data != nil {
		result.Data = &models.SignatureVerificationData{
			SimilarityPercentage: apiResponse.Data.SimilarityPercentage,
			Classification:       apiResponse.Data.Classification,
		}
	}

	if apiResponse.Success {
		result.Status = "completed"
		if apiResponse.Status != "" {
			result.Status = apiResponse.Status
		}
		result.Message = "Signature verification completed successfully"
		if strings.TrimSpace(apiResponse.Message) != "" {
			result.Message = strings.TrimSpace(apiResponse.Message)
		}
	} else {
		result.Status = "failed"
		if apiResponse.Status != "" {
			result.Status = apiResponse.Status
		}
		switch {
		case apiResponse.ErrorMessage != nil && strings.TrimSpace(*apiResponse.ErrorMessage) != "":
			result.Message = strings.TrimSpace(*apiResponse.ErrorMessage)
		case strings.TrimSpace(apiResponse.Message) != "":
			result.Message = strings.TrimSpace(apiResponse.Message)
		default:
			result.Message = "Signature verification failed with unknown error"
			log.Printf("Signature Verification API failure with no message field: %s", truncateForLog(string(body), 500))
		}
	}

	log.Printf("Signature Verification API result: Success=%t, Status=%s, Message=%s",
		result.Success, result.Status, result.Message)

	if result.Data != nil {
		log.Printf("Verification Data: Similarity=%.2f%%, Classification=%s",
			result.Data.SimilarityPercentage, result.Data.Classification)
	}

	return result, nil
}

func truncateForLog(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}
