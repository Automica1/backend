// internal/models/face_detection.go
package models

import (
	"errors"
	"strings"
	"time"
)

// Face Detection request structure - matches the API expectations
type FaceDetectionRequest struct {
	ReqID     string `json:"req_id" validate:"required"`
	DocBase64 string `json:"doc_base64" validate:"required"`
}

// Face Detection result structure (returned by external API)
type FaceDetectionResult struct {
	ReqID   string   `json:"req_id"`
	Success bool     `json:"success"`
	Status  string   `json:"status"`
	Data    []string `json:"data,omitempty"`    // Array of base64 cropped faces
	Message string   `json:"message,omitempty"`
}

// Face Detection response structure (returned to client)
type FaceDetectionResponse struct {
	Message          string               `json:"message"`
	UserID           string               `json:"userId"`
	RemainingCredits int                  `json:"remainingCredits"`
	FaceResult       *FaceDetectionResult `json:"faceResult"`
	ProcessedAt      time.Time            `json:"processedAt"`
}

func (r *FaceDetectionRequest) Validate() error {
	if strings.TrimSpace(r.ReqID) == "" {
		return errors.New("req_id is required and cannot be empty")
	}
	if strings.TrimSpace(r.DocBase64) == "" {
		return errors.New("doc_base64 is required and cannot be empty")
	}
	
	// Basic validation for base64 string
	if len(r.DocBase64) < 10 {
		return errors.New("doc_base64 appears to be too short to be a valid document")
	}
	
	return nil
}