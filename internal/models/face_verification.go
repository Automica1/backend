// internal/models/face_verification.go
package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Face Verification request structure - matches the new API expectations
type FaceVerificationRequest struct {
	ReqID     string   `json:"req_id" validate:"required"`
	DocBase64 []string `json:"doc_base64" validate:"required"`
}

// Face Verification data structure (nested in API response)
type FaceVerificationData struct {
	SimilarityPercentage float64 `json:"similarity_percentage"`
	Classification       string  `json:"classification"`
}

// Face Verification result structure (returned by external API)
type FaceVerificationResult struct {
	ReqID   string                `json:"req_id"`
	Success bool                  `json:"success"`
	Status  string                `json:"status"`
	Data    *FaceVerificationData `json:"data,omitempty"`
	Message string                `json:"message,omitempty"`
}

// Face Verification response structure (returned to client)
type FaceVerificationResponse struct {
	Message          string                  `json:"message"`
	UserID           string                  `json:"userId"`
	RemainingCredits int                     `json:"remainingCredits"`
	FaceResult       *FaceVerificationResult `json:"faceResult"`
	ProcessedAt      time.Time               `json:"processedAt"`
}

func (r *FaceVerificationRequest) Validate() error {
	if strings.TrimSpace(r.ReqID) == "" {
		return errors.New("req_id is required and cannot be empty")
	}
	if len(r.DocBase64) == 0 {
		return errors.New("doc_base64 is required and cannot be empty")
	}

	// Validate each base64 string
	for i, doc := range r.DocBase64 {
		if strings.TrimSpace(doc) == "" {
			return fmt.Errorf("doc_base64[%d] cannot be empty", i)
		}
		// Basic validation for base64 strings
		if len(doc) < 10 {
			return fmt.Errorf("doc_base64[%d] appears to be too short to be a valid document", i)
		}
	}

	return nil
}
