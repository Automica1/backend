// internal/services/face_verification_service.go
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
	apperrors "chi-mongo-backend/pkg/errors"
)

type FaceVerificationAPIService interface {
	ProcessFaceVerification(ctx context.Context, req *models.FaceVerificationRequest) (*models.FaceVerificationResult, error)
}

type faceVerificationAPIService struct {
	httpClient  *http.Client
	apiURL      string
	errorMapper *apperrors.APIErrorMapper
}

func NewFaceVerificationAPIService() FaceVerificationAPIService {
	return &faceVerificationAPIService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL:      getFaceVerificationAPIURL(),
		errorMapper: apperrors.NewAPIErrorMapper(),
	}
}

func (s *faceVerificationAPIService) ProcessFaceVerification(ctx context.Context, req *models.FaceVerificationRequest) (*models.FaceVerificationResult, error) {
	// Prepare the request payload exactly as expected by the new API
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

	// Parse the raw response to preserve original structure
	var rawResponse map[string]interface{}
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("failed to parse raw response: %w", err)
	}

	// Parse the response - exact format from new API specification
	var apiResponse struct {
		ReqID   string `json:"req_id"`
		Success bool   `json:"success"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    *struct {
			SimilarityPercentage float64 `json:"similarity_percentage"`
			Classification       string  `json:"classification"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, apperrors.NewAppErrorWithOriginalResponse(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"Failed to parse API response",
			rawResponse,
			err.Error(),
		)
	}

	// Check for HTTP errors first
	if resp.StatusCode != http.StatusOK {
		errorMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		return nil, apperrors.NewAPIErrorWithOriginalResponse(
			s.errorMapper,
			errorMsg,
			rawResponse,
		)
	}

	// Create result based on API response
	result := &models.FaceVerificationResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
		Status:  apiResponse.Status,
		Message: apiResponse.Message,
	}

	// Include verification data if available
	if apiResponse.Data != nil {
		result.Data = &models.FaceVerificationData{
			SimilarityPercentage: apiResponse.Data.SimilarityPercentage,
			Classification:       apiResponse.Data.Classification,
		}
	}

	// If API call failed, return error with original response
	if !apiResponse.Success {
		return result, apperrors.NewAPIErrorWithOriginalResponse(
			s.errorMapper,
			result.Message,
			rawResponse,
		)
	}

	log.Printf("Face Verification API result: Success=%t, Status=%s, Message=%s", result.Success, result.Status, result.Message)
	if result.Data != nil {
		log.Printf("Verification Data: SimilarityPercentage=%.6f, Classification=%s", result.Data.SimilarityPercentage, result.Data.Classification)
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
