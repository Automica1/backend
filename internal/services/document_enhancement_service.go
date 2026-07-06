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
	"time"

	"chi-mongo-backend/internal/models"
)

type DocumentEnhancementAPIService interface {
	ProcessDocumentEnhancement(ctx context.Context, req *models.DocumentEnhancementRequest) (*models.DocumentEnhancementResult, error)
}

type documentEnhancementAPIService struct {
	httpClient *http.Client
	apiURL     string
}

func NewDocumentEnhancementAPIService() DocumentEnhancementAPIService {
	return &documentEnhancementAPIService{
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		apiURL: getDocumentEnhancementAPIURL(),
	}
}

func (s *documentEnhancementAPIService) ProcessDocumentEnhancement(ctx context.Context, req *models.DocumentEnhancementRequest) (*models.DocumentEnhancementResult, error) {
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

	log.Printf("Making Document Enhancement API request to: %s", s.apiURL)
	log.Printf("Document Enhancement request ReqID=%s payload_bytes=%d", req.ReqID, len(jsonData))

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call document enhancement API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("Document Enhancement API response status: %d bytes=%d", resp.StatusCode, len(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("document enhancement API returned non-OK status %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse struct {
		ReqID        string                 `json:"req_id"`
		Success      bool                   `json:"success"`
		ErrorMessage *string                `json:"error_message"`
		Result       *string                `json:"result"`
		Data         map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	result := &models.DocumentEnhancementResult{
		ReqID:           apiResponse.ReqID,
		Success:         apiResponse.Success,
		EnhancementData: apiResponse.Data,
	}

	if apiResponse.Success {
		result.Status = "completed"
		result.Result = apiResponse.Result
		result.Message = "Document enhancement completed successfully"
	} else {
		result.Status = "failed"
		if apiResponse.ErrorMessage != nil {
			result.Message = *apiResponse.ErrorMessage
		} else {
			result.Message = "Document enhancement failed with unknown error"
		}

		originalResponse := map[string]interface{}{
			"req_id":        apiResponse.ReqID,
			"success":       apiResponse.Success,
			"error_message": "",
			"result":        "",
		}
		if apiResponse.ErrorMessage != nil {
			originalResponse["error_message"] = *apiResponse.ErrorMessage
		}
		if apiResponse.Result != nil {
			originalResponse["result"] = *apiResponse.Result
		}
		if apiResponse.Data != nil {
			originalResponse["data"] = apiResponse.Data
		}
		result.OriginalResponse = originalResponse
	}

	return result, nil
}

func getDocumentEnhancementAPIURL() string {
	value := os.Getenv("DOC_ENHANCE_API_URL")
	if value == "" {
		log.Fatalf("Environment variable DOC_ENHANCE_API_URL is not set")
	}
	return value
}
