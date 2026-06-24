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
	"time"

	"chi-mongo-backend/internal/models"
)

type SignatureVerificationProcessOptions struct {
	UseBeta bool
}

type SignatureVerificationAPIService interface {
	ProcessSignatureVerification(ctx context.Context, req *models.SignatureVerificationRequest, opts *SignatureVerificationProcessOptions) (*models.SignatureVerificationResult, error)
}

type signatureVerificationAPIService struct {
	httpClient *http.Client
	prodURL    string
	betaURL    string
	gatewayKey string
}

func NewSignatureVerificationAPIService() SignatureVerificationAPIService {
	return &signatureVerificationAPIService{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		prodURL:    getEnvOrDefault("VERIFY_SIGNATURE_API_URL"),
		betaURL:    os.Getenv("VERIFY_SIGNATURE_BETA_API_URL"),
		gatewayKey: os.Getenv("BETA_ML_GATEWAY_KEY"),
	}
}

func (s *signatureVerificationAPIService) ProcessSignatureVerification(ctx context.Context, req *models.SignatureVerificationRequest, opts *SignatureVerificationProcessOptions) (*models.SignatureVerificationResult, error) {
	useBeta := opts != nil && opts.UseBeta
	apiURL := s.prodURL
	if useBeta {
		if s.betaURL == "" {
			return nil, fmt.Errorf("beta signature verification is not configured")
		}
		apiURL = s.betaURL
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

	resp, err := s.httpClient.Do(httpReq)
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
		return nil, fmt.Errorf("signature verification API returned non-OK status %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse struct {
		ReqID        string  `json:"req_id"`
		Success      bool    `json:"success"`
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

	if apiResponse.Success {
		result.Status = "completed"
		result.Message = "Signature verification completed successfully"

		if apiResponse.Data != nil {
			result.Data = &models.SignatureVerificationData{
				SimilarityPercentage: apiResponse.Data.SimilarityPercentage,
				Classification:       apiResponse.Data.Classification,
			}
		}
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "Signature verification failed with unknown error"
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
