// internal/services/qr_masking_service.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
	"log"

	"chi-mongo-backend/internal/models"
)

type QRMaskingAPIService interface {
	ProcessQRMasking(ctx context.Context, req *models.QRMaskingRequest) (*models.QRMaskingResult, error)
}

type qrMaskingAPIService struct {
	httpClient *http.Client
	apiURL     string
}

// Only use real API service
func NewQRMaskingAPIService() QRMaskingAPIService {
	return &qrMaskingAPIService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: getEnvOrDefault("QR_MASKING_API_URL"),
	}
}

func (s *qrMaskingAPIService) ProcessQRMasking(ctx context.Context, req *models.QRMaskingRequest) (*models.QRMaskingResult, error) {
	// Prepare the request payload exactly as expected by the API
	payload := map[string]interface{}{
		"req_id":     req.ReqID,
		"base64_str": req.Base64Str,
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

	// Log the request for debugging
	log.Printf("Making QR API request to: %s", s.apiURL)
	log.Printf("Request payload: %s", string(jsonData))

	// Make the API call
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call QR masking API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the response for debugging
	log.Printf("QR API response status: %d", resp.StatusCode)
	log.Printf("QR API response body: %s", string(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("QR masking API returned non-OK status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response - exact format from API specification
	var apiResponse struct {
		ReqID        string `json:"req_id"`
		Success      bool   `json:"success"`
		ErrorMessage *string `json:"error_message"`
		MaskedBase64 string `json:"masked_base64"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Create result based on API response
	result := &models.QRMaskingResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
	}

	if apiResponse.Success {
		result.Status = "completed"
		result.MaskedBase64 = apiResponse.MaskedBase64
		result.Message = "QR masking completed successfully"
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "QR masking failed with unknown error"
		}
	}

	log.Printf("QR API result: Success=%t, Status=%s, Message=%s", result.Success, result.Status, result.Message)

	return result, nil
}

func getEnvOrDefault(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("Environment variable %s is not set", key)
	}
	return value
}