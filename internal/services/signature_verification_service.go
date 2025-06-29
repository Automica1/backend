// internal/services/signature_verification_service.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"log"

	"chi-mongo-backend/internal/models"
)

type SignatureVerificationAPIService interface {
	ProcessSignatureVerification(ctx context.Context, req *models.SignatureVerificationRequest) (*models.SignatureVerificationResult, error)
}

type signatureVerificationAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewSignatureVerificationAPIService() SignatureVerificationAPIService {
	return &signatureVerificationAPIService{
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Longer timeout for signature verification
		},
		apiURL: getEnvOrDefault("VERIFY_SIGNATURE_API_URL"),
	}
}

func (s *signatureVerificationAPIService) ProcessSignatureVerification(ctx context.Context, req *models.SignatureVerificationRequest) (*models.SignatureVerificationResult, error) {
	// Prepare the request payload exactly as expected by the API
	payload := map[string]interface{}{
		"req_id":     req.ReqID,
		"doc_base64": req.DocBase64,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	// Log the request for debugging (without full base64 data)
	log.Printf("Making Signature Verification API request to: %s", s.apiURL)
	log.Printf("Request ReqID: %s, DocBase64 count: %d", req.ReqID, len(req.DocBase64))

	// Make the API call
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call signature verification API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the response for debugging
	log.Printf("Signature Verification API response status: %d", resp.StatusCode)
	log.Printf("Signature Verification API response body: %s", string(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signature verification API returned non-OK status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response - exact format from API specification
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

	// Create result based on API response
	result := &models.SignatureVerificationResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
	}

	if apiResponse.Success {
		result.Status = "completed"
		result.Message = "Signature verification completed successfully"
		
		// Add verification data if available
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
