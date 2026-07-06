package models

import (
	"errors"
	"strings"
	"time"
)

type DocumentEnhancementRequest struct {
	ReqID     string `json:"req_id" validate:"required"`
	DocBase64 string `json:"doc_base64" validate:"required"`
}

type DocumentEnhancementResult struct {
	ReqID            string                 `json:"req_id"`
	Success          bool                   `json:"success"`
	Status           string                 `json:"status"`
	Result           *string                `json:"result,omitempty"`
	Message          string                 `json:"message,omitempty"`
	EnhancementData  map[string]interface{} `json:"data,omitempty"`
	OriginalResponse map[string]interface{} `json:"original_response,omitempty"`
}

type DocumentEnhancementResponse struct {
	Message          string                     `json:"message"`
	UserID           string                     `json:"userId"`
	RemainingCredits int                        `json:"remainingCredits"`
	EnhanceResult    *DocumentEnhancementResult `json:"enhanceResult"`
	ProcessedAt      time.Time                  `json:"processedAt"`
}

func (r *DocumentEnhancementRequest) Validate() error {
	if strings.TrimSpace(r.ReqID) == "" {
		return errors.New("req_id is required and cannot be empty")
	}
	if strings.TrimSpace(r.DocBase64) == "" {
		return errors.New("doc_base64 is required and cannot be empty")
	}
	if len(r.DocBase64) < 10 {
		return errors.New("doc_base64 appears to be too short to be a valid document")
	}
	return nil
}
