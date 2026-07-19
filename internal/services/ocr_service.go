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
	"strconv"
	"time"

	"chi-mongo-backend/internal/models"
)

const defaultOCRAPITimeout = 300 * time.Second

type OCRAPIService interface {
	ProcessOCR(ctx context.Context, req *models.OCRRequest) (*models.OCRResult, error)
}

type ocrAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewOCRAPIService() OCRAPIService {
	return &ocrAPIService{
		httpClient: &http.Client{
			Timeout: OCRAPIRequestTimeout(),
		},
		apiURL: getOCRAPIURL(),
	}
}

func (s *ocrAPIService) ProcessOCR(ctx context.Context, req *models.OCRRequest) (*models.OCRResult, error) {
	payload := map[string]interface{}{
		"req_id":     req.ReqID,
		"doc_base64": req.DocBase64,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Printf("Making OCR API request to: %s", s.apiURL)
	log.Printf("OCR request ReqID=%s payload_bytes=%d", req.ReqID, len(jsonData))

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call OCR API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("OCR API response status: %d bytes=%d", resp.StatusCode, len(body))

	var result models.OCRResult
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("OCR API returned status %d with unparseable response: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("failed to parse OCR API response: %w", err)
	}

	normalizeOCRResult(&result, req.ReqID, resp.StatusCode)

	if resp.StatusCode != http.StatusOK && result.Success {
		return nil, fmt.Errorf("OCR API returned status %d for successful response", resp.StatusCode)
	}

	return &result, nil
}

func normalizeOCRResult(result *models.OCRResult, reqID string, upstreamStatus int) {
	if result.ReqID == "" {
		result.ReqID = reqID
	}
	result.UpstreamStatus = upstreamStatus
	if result.Data.Blocks == nil {
		result.Data.Blocks = []map[string]interface{}{}
	}
	if result.Status == "" {
		if result.Success {
			result.Status = "completed"
		} else {
			result.Status = "failed"
		}
	}
	if result.Message == "" {
		if result.Success {
			result.Message = "Document extraction completed successfully"
		} else {
			result.Message = "OCR operation failed"
		}
	}
}

func getOCRAPIURL() string {
	if value := os.Getenv("OCR_API_URL"); value != "" {
		return value
	}
	return "https://api.automica.ai/v1/ocr/gpu"
}

func OCRAPIRequestTimeout() time.Duration {
	value := os.Getenv("OCR_API_TIMEOUT_SECONDS")
	if value == "" {
		return defaultOCRAPITimeout
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		log.Printf("Invalid OCR_API_TIMEOUT_SECONDS=%q; using default %s", value, defaultOCRAPITimeout)
		return defaultOCRAPITimeout
	}
	return time.Duration(seconds) * time.Second
}
