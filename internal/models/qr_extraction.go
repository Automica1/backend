// internal/models/qr_extraction.go
package models

import (
	"errors"
	"strings"
	"time"
)

// QR Extraction request structure - matches the API expectations
type QRExtractionRequest struct {
	ReqID      string `json:"req_id" validate:"required"`
	DocBase64  string `json:"doc_base64" validate:"required"`
}

// QR Extraction result structure (returned by external API)
type QRExtractionResult struct {
	ReqID   string  `json:"req_id"`
	Success bool    `json:"success"`
	Status  string  `json:"status"`
	Result  *string `json:"result,omitempty"`
	Message string  `json:"message,omitempty"`
}

// QR Extraction response structure (returned to client)
type QRExtractionResponse struct {
	Message          string              `json:"message"`
	UserID           string              `json:"userId"`
	RemainingCredits int                 `json:"remainingCredits"`
	QRResult         *QRExtractionResult `json:"qrResult"`
	ProcessedAt      time.Time           `json:"processedAt"`
}

func (r *QRExtractionRequest) Validate() error {
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