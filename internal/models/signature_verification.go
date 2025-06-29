// internal/models/signature_verification.go
package models

import (
	"errors"
	"time"
)

// SignatureVerificationRequest represents the request payload for signature verification
type SignatureVerificationRequest struct {
	ReqID      string   `json:"req_id" bson:"req_id"`
	DocBase64  []string `json:"doc_base64" bson:"doc_base64"`
}

// Validate validates the signature verification request
func (r *SignatureVerificationRequest) Validate() error {
	if r.ReqID == "" {
		return errors.New("req_id is required")
	}

	if len(r.DocBase64) == 0 {
		return errors.New("doc_base64 array cannot be empty")
	}

	if len(r.DocBase64) > 10 { // reasonable limit
		return errors.New("doc_base64 array cannot contain more than 10 images")
	}

	for _, base64Str := range r.DocBase64 {
		if base64Str == "" {
			return errors.New("doc_base64 array cannot contain empty base64 strings")
		}
		if len(base64Str) > 10*1024*1024 { // 10MB limit per image
			return errors.New("base64 string too large (max 10MB per image)")
		}
	}

	return nil
}

// SignatureVerificationResult represents the result from the signature verification API
type SignatureVerificationResult struct {
	ReqID                   string                        `json:"req_id" bson:"req_id"`
	Success                 bool                         `json:"success" bson:"success"`
	Status                  string                       `json:"status" bson:"status"`
	Message                 string                       `json:"message" bson:"message"`
	Data                    *SignatureVerificationData   `json:"data,omitempty" bson:"data,omitempty"`
}

// SignatureVerificationData represents the verification data from the API
type SignatureVerificationData struct {
	SimilarityPercentage float64 `json:"similarity_percentage" bson:"similarity_percentage"`
	Classification       string  `json:"classification" bson:"classification"`
}

// SignatureVerificationResponse represents the API response to the client
type SignatureVerificationResponse struct {
	Message          string                       `json:"message"`
	UserID           string                       `json:"user_id"`
	RemainingCredits int                         `json:"remaining_credits"`
	VerificationResult *SignatureVerificationResult `json:"verification_result"`
	ProcessedAt      time.Time                   `json:"processed_at"`
}
