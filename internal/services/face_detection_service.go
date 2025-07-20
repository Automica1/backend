// internal/services/face_detection_service.go
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

type FaceDetectionAPIService interface {
	ProcessFaceDetection(ctx context.Context, req *models.FaceDetectionRequest) (*models.FaceDetectionResult, error)
}

type faceDetectionAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewFaceDetectionAPIService() FaceDetectionAPIService {
	return &faceDetectionAPIService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: getFaceDetectionAPIURL(),
	}
}

func (s *faceDetectionAPIService) ProcessFaceDetection(ctx context.Context, req *models.FaceDetectionRequest) (*models.FaceDetectionResult, error) {
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
	log.Printf("Making Face Detection API request to: %s", s.apiURL)
	log.Printf("Request payload: %s", string(jsonData))

	// Make the API call
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call face detection API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the response for debugging
	log.Printf("Face Detection API response status: %d", resp.StatusCode)
	log.Printf("Face Detection API response body: %s", string(body))

	// Parse the response - exact format from API specification
	var apiResponse struct {
		ReqID        string    `json:"req_id"`
		Success      bool      `json:"success"`
		ErrorMessage *string   `json:"error_message"`
		Data         []string  `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		// If we can't parse the response, create a generic error result
		return &models.FaceDetectionResult{
			ReqID:   req.ReqID,
			Success: false,
			Status:  "failed",
			Message: "Failed to parse API response",
			Data:    []string{},
		}, nil
	}

	// Create result based on API response - ALWAYS return a result, never an error for API failures
	result := &models.FaceDetectionResult{
		ReqID:   apiResponse.ReqID,
		Success: apiResponse.Success,
		Data:    apiResponse.Data,
	}

	if apiResponse.Success {
		result.Status = "completed"
		if len(apiResponse.Data) == 0 {
			result.Message = "Face detection completed but no faces found"
		} else {
			result.Message = fmt.Sprintf("Face detection completed successfully, found %d face(s)", len(apiResponse.Data))
		}
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "Face detection failed with unknown error"
		}
	}

	// Don't check HTTP status code here - let the handler deal with success/failure logic
	// The API might return 400 with a structured error response, which is still valid
	log.Printf("Face Detection API result: Success=%t, Status=%s, Message=%s, Faces=%d", 
		result.Success, result.Status, result.Message, len(result.Data))

	return result, nil
}

func getFaceDetectionAPIURL() string {
	value := os.Getenv("FACE_DETECTION_API_URL")
	if value == "" {
		log.Fatalf("Environment variable FACE_DETECTION_API_URL is not set")
	}
	return value
}