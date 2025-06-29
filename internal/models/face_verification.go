// internal/models/face_verification.go
package models

import (
	"errors"
	"strings"
	"time"
)

// Face Verification request structure - matches the API expectations
type FaceVerificationRequest struct {
	ReqID       string `json:"req_id" validate:"required"`
	DocBase64_1 string `json:"doc_base64_1" validate:"required"`
	DocBase64_2 string `json:"doc_base64_2" validate:"required"`
	DocType     string `json:"doc_type" validate:"required"`
}

// Face Verification data structure (nested in API response)
type FaceVerificationData struct {
	Confidence float64 `json:"confidence"`
	Verified   bool    `json:"verified"`
}

// Face Verification result structure (returned by external API)
type FaceVerificationResult struct {
	ReqID   string                    `json:"req_id"`
	Success bool                      `json:"success"`
	Status  string                    `json:"status"`
	Data    *FaceVerificationData     `json:"data,omitempty"`
	Message string                    `json:"message,omitempty"`
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
	if strings.TrimSpace(r.DocBase64_1) == "" {
		return errors.New("doc_base64_1 is required and cannot be empty")
	}
	if strings.TrimSpace(r.DocBase64_2) == "" {
		return errors.New("doc_base64_2 is required and cannot be empty")
	}
	if strings.TrimSpace(r.DocType) == "" {
		return errors.New("doc_type is required and cannot be empty")
	}
	
	// Validate doc_type (must be "face" according to API spec)
	if strings.ToLower(strings.TrimSpace(r.DocType)) != "face" {
		return errors.New("doc_type must be 'face'")
	}
	
	// Basic validation for base64 strings
	if len(r.DocBase64_1) < 10 {
		return errors.New("doc_base64_1 appears to be too short to be a valid document")
	}
	if len(r.DocBase64_2) < 10 {
		return errors.New("doc_base64_2 appears to be too short to be a valid document")
	}
	
	return nil
}