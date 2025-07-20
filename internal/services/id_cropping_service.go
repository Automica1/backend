// internal/services/id_cropping_service.go
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

type IDCroppingAPIService interface {
	ProcessIDCropping(ctx context.Context, req *models.IDCroppingRequest) (*models.IDCroppingResult, error)
}

type idCroppingAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewIDCroppingAPIService() IDCroppingAPIService {
	return &idCroppingAPIService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: getIDCroppingAPIURL(),
	}
}

func (s *idCroppingAPIService) ProcessIDCropping(ctx context.Context, req *models.IDCroppingRequest) (*models.IDCroppingResult, error) {
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
	log.Printf("Making ID Cropping API request to: %s", s.apiURL)
	log.Printf("Request ReqID: %s, DocBase64 length: %d", req.ReqID, len(req.DocBase64))

	// Make the API call
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call ID cropping API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the response for debugging
	log.Printf("ID Cropping API response status: %d", resp.StatusCode)
	log.Printf("ID Cropping API response body: %s", string(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ID cropping API returned non-OK status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response - exact format from API specification
	var apiResponse struct {
		ReqID        string  `json:"req_id"`
		Success      bool    `json:"success"`
		ErrorMessage *string `json:"error_message"`
		Result       *string `json:"result"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Create result based on API response
	result := &models.IDCroppingResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
	}

	if apiResponse.Success {
		result.Status = "completed"
		result.Result = apiResponse.Result
		result.Message = "ID cropping completed successfully"
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "ID cropping failed with unknown error"
		}
		
		// IMPORTANT: Store the original response exactly as received from the backend
		// This ensures consistency with what the handler expects
		originalResponse := make(map[string]interface{})
		originalResponse["req_id"] = apiResponse.ReqID
		originalResponse["success"] = apiResponse.Success
		
		// Handle error_message - ensure it's included even if nil
		if apiResponse.ErrorMessage != nil {
			originalResponse["error_message"] = *apiResponse.ErrorMessage
		} else {
			originalResponse["error_message"] = nil
		}
		
		// Handle result - ensure it's included even if nil, matching backend format
		if apiResponse.Result != nil {
			originalResponse["result"] = *apiResponse.Result
		} else {
			// Backend returns empty string for failed requests, not nil
			originalResponse["result"] = ""
		}
		
		result.OriginalResponse = originalResponse
	}

	log.Printf("ID Cropping API result: Success=%t, Status=%s, Message=%s", result.Success, result.Status, result.Message)

	return result, nil
}

func getIDCroppingAPIURL() string {
	value := os.Getenv("ID_CROPPING_API_URL")
	if value == "" {
		log.Fatalf("Environment variable ID_CROPPING_API_URL is not set")
	}
	return value
}