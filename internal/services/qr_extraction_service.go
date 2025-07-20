// internal/services/qr_extraction_service.go
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

type QRExtractionAPIService interface {
	ProcessQRExtraction(ctx context.Context, req *models.QRExtractionRequest) (*models.QRExtractionResult, error)
}

type qrExtractionAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewQRExtractionAPIService() QRExtractionAPIService {
	return &qrExtractionAPIService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: getQRExtractionAPIURL(),
	}
}

func (s *qrExtractionAPIService) ProcessQRExtraction(ctx context.Context, req *models.QRExtractionRequest) (*models.QRExtractionResult, error) {
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

	// Log the request for debugging
	log.Printf("Making QR Extraction API request to: %s", s.apiURL)
	log.Printf("Request payload: %s", string(jsonData))

	// Make the API call
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call QR extraction API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the response for debugging
	log.Printf("QR Extraction API response status: %d", resp.StatusCode)
	log.Printf("QR Extraction API response body: %s", string(body))

	// Handle non-OK HTTP status codes
	if resp.StatusCode != http.StatusOK {
		// Try to parse error response for better error handling
		var errorResponse map[string]interface{}
		if parseErr := json.Unmarshal(body, &errorResponse); parseErr == nil {
			// Return structured error result
			result := &models.QRExtractionResult{
				ReqID:   req.ReqID,
				Success: false,
				Status:  "failed",
				Message: fmt.Sprintf("API returned status %d", resp.StatusCode),
			}
			
			// Extract error message if available
			if errorMsg, exists := errorResponse["error_message"]; exists {
				if errorStr, ok := errorMsg.(string); ok {
					result.Message = errorStr
				}
			} else if message, exists := errorResponse["message"]; exists {
				if messageStr, ok := message.(string); ok {
					result.Message = messageStr
				}
			}
			
			return result, nil
		}
		
		// Fallback for unparseable error responses
		return &models.QRExtractionResult{
			ReqID:   req.ReqID,
			Success: false,
			Status:  "failed",
			Message: fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(body)),
		}, nil
	}

	// Parse the successful response - exact format from API specification
	var apiResponse struct {
		ReqID        string  `json:"req_id"`
		Success      bool    `json:"success"`
		ErrorMessage *string `json:"error_message"`
		Result       *string `json:"result"`
		Data         interface{} `json:"data,omitempty"` // Additional data field
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return &models.QRExtractionResult{
			ReqID:   req.ReqID,
			Success: false,
			Status:  "failed",
			Message: fmt.Sprintf("failed to parse API response: %v", err),
		}, nil
	}

	// Create result based on API response
	result := &models.QRExtractionResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
	}

	if apiResponse.Success {
		result.Status = "completed"
		result.Result = apiResponse.Result
		result.Message = "QR extraction completed successfully"
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil && *apiResponse.ErrorMessage != "" {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "QR extraction failed with unknown error"
		}
	}

	log.Printf("QR Extraction API result: Success=%t, Status=%s, Message=%s", result.Success, result.Status, result.Message)

	return result, nil
}

func getQRExtractionAPIURL() string {
	value := os.Getenv("QR_EXTRACT_API_URL")
	if value == "" {
		log.Fatalf("Environment variable QR_EXTRACT_API_URL is not set")
	}
	return value
}