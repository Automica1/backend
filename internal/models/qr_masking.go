// internal/models/qr_masking.go
package models

import (
	"errors"
	"strings"
	"time"
)

// QR Masking request structure - matches the API expectations
type QRMaskingRequest struct {
	ReqID     string `json:"req_id" validate:"required"`
	Base64Str string `json:"base64_str" validate:"required"`
}

// QR Masking result structure (returned by external API)
type QRMaskingResult struct {
	ReqID        string `json:"req_id"`
	Success      bool   `json:"success"`
	Status       string `json:"status"`
	MaskedBase64 string `json:"masked_base64,omitempty"`
	Message      string `json:"message,omitempty"`
}

// QR Masking response structure (returned to client)
type QRMaskingResponse struct {
	Message          string             `json:"message"`
	UserID           string             `json:"userId"`
	RemainingCredits int                `json:"remainingCredits"`
	QRResult         *QRMaskingResult   `json:"qrResult"`
	ProcessedAt      time.Time          `json:"processedAt"`
}

func (r *QRMaskingRequest) Validate() error {
	if strings.TrimSpace(r.ReqID) == "" {
		return errors.New("req_id is required and cannot be empty")
	}
	if strings.TrimSpace(r.Base64Str) == "" {
		return errors.New("base64_str is required and cannot be empty")
	}
	
	// Basic validation for base64 string
	if len(r.Base64Str) < 10 {
		return errors.New("base64_str appears to be too short to be a valid image")
	}
	
	return nil
}