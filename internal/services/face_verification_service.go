// internal/services/face_verification_service.go
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

type FaceVerificationAPIService interface {
	ProcessFaceVerification(ctx context.Context, req *models.FaceVerificationRequest) (*models.FaceVerificationResult, error)
}

type faceVerificationAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewFaceVerificationAPIService() FaceVerificationAPIService {
	return &faceVerificationAPIService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: getFaceVerificationAPIURL(),
	}
}

func (s *faceVerificationAPIService) ProcessFaceVerification(ctx context.Context, req *models.FaceVerificationRequest) (*models.FaceVerificationResult, error) {
	// Prepare the request payload exactly as expected by the API
	payload := map[string]interface{}{
		"req_id":       req.ReqID,
		"doc_base64_1": req.DocBase64_1,
		"doc_base64_2": req.DocBase64_2,
		"doc_type":     req.DocType,
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
	log.Printf("Making Face Verification API request to: %s", s.apiURL)
	log.Printf("Request payload: %s", string(jsonData))

	// Make the API call
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call face verification API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the response for debugging
	log.Printf("Face Verification API response status: %d", resp.StatusCode)
	log.Printf("Face Verification API response body: %s", string(body))

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("face verification API returned non-OK status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response - exact format from API specification
	var apiResponse struct {
		ReqID        string  `json:"req_id"`
		Success      bool    `json:"success"`
		ErrorMessage *string `json:"error_message"`
		Data         *struct {
			Confidence float64 `json:"confidence"`
			Verified   bool    `json:"verified"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Create result based on API response
	result := &models.FaceVerificationResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
	}

	if apiResponse.Success {
		result.Status = "completed"
		result.Message = "Face verification completed successfully"
		
		// Include verification data if available
		if apiResponse.Data != nil {
			result.Data = &models.FaceVerificationData{
				Confidence: apiResponse.Data.Confidence,
				Verified:   apiResponse.Data.Verified,
			}
		}
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "Face verification failed with unknown error"
		}
	}

	log.Printf("Face Verification API result: Success=%t, Status=%s, Message=%s", result.Success, result.Status, result.Message)
	if result.Data != nil {
		log.Printf("Verification Data: Verified=%t, Confidence=%.6f", result.Data.Verified, result.Data.Confidence)
	}

	return result, nil
}

func getFaceVerificationAPIURL() string {
	value := os.Getenv("FACE_VERIFICATION_API_URL")
	if value == "" {
		log.Fatalf("Environment variable FACE_VERIFICATION_API_URL is not set")
	}
	return value
}