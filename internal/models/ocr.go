package models

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	OCRServiceName   = "ocr"
	OCRGPUServiceTag = "ocr-gpu"
)

type OCRRequest struct {
	ReqID     string `json:"req_id" validate:"required"`
	DocBase64 string `json:"doc_base64" validate:"required"`
}

type OCRData struct {
	Text   string                   `json:"text"`
	Blocks []map[string]interface{} `json:"blocks"`
}

type OCRResult struct {
	ReqID          string  `json:"req_id"`
	Success        bool    `json:"success"`
	Status         string  `json:"status"`
	Message        string  `json:"message,omitempty"`
	Data           OCRData `json:"data"`
	UpstreamStatus int     `json:"-"`
}

type OCRResponse struct {
	Message          string     `json:"message"`
	UserID           string     `json:"userId"`
	RemainingCredits int        `json:"remainingCredits"`
	OCRResult        *OCRResult `json:"ocrResult"`
	ProcessedAt      time.Time  `json:"processedAt"`
}

type OCRGPURequiredResponse struct {
	Success    bool         `json:"success"`
	Status     string       `json:"status"`
	Message    string       `json:"message"`
	ServiceTag string       `json:"serviceTag"`
	State      GPUPoolState `json:"state"`
	UserActive bool         `json:"userActive"`
	PollURL    string       `json:"pollUrl,omitempty"`
}

func (r *OCRRequest) Validate() error {
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

// ValidateWithPolicy applies the request-level policy checks that are cheap and reliable
// at request time. Page-count enforcement is intentionally deferred because the current
// OCR request contract does not expose a trustworthy page count without a heavier parser.
func (r *OCRRequest) ValidateWithPolicy(policy *ServicePolicy) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if policy == nil || policy.Limits == nil || policy.Limits.MaxUploadSizeMB == nil {
		return nil
	}

	sizeBytes, err := r.decodedDocumentSizeBytes()
	if err != nil {
		return err
	}
	maxBytes := *policy.Limits.MaxUploadSizeMB * 1024 * 1024
	if sizeBytes > maxBytes {
		return fmt.Errorf("document exceeds max upload size of %d MB", *policy.Limits.MaxUploadSizeMB)
	}
	return nil
}

func (r *OCRRequest) decodedDocumentSizeBytes() (int, error) {
	payload := strings.TrimSpace(r.DocBase64)
	if idx := strings.Index(payload, ","); idx > 0 {
		prefix := strings.ToLower(payload[:idx])
		if strings.HasPrefix(prefix, "data:") && strings.Contains(prefix, ";base64") {
			payload = payload[idx+1:]
		}
	}
	if payload == "" {
		return 0, errors.New("doc_base64 is required and cannot be empty")
	}

	for _, decoder := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	} {
		if decoded, err := decoder(payload); err == nil {
			return len(decoded), nil
		}
	}
	return 0, errors.New("doc_base64 is not valid base64")
}
