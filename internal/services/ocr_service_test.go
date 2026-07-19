package services

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"
)

func TestOCRAPIRequestTimeoutDefaultAndEnv(t *testing.T) {
	t.Setenv("OCR_API_TIMEOUT_SECONDS", "")
	if got := OCRAPIRequestTimeout(); got != 300*time.Second {
		t.Fatalf("default OCR timeout = %s, want 300s", got)
	}

	t.Setenv("OCR_API_TIMEOUT_SECONDS", "900")
	if got := OCRAPIRequestTimeout(); got != 900*time.Second {
		t.Fatalf("env OCR timeout = %s, want 900s", got)
	}
}

func TestOCRAPIServiceReturnsStructuredFailureForNonOK(t *testing.T) {
	svc := &ocrAPIService{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/document/extract" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"req_id":"req-1",
						"success":false,
						"status":"failed",
						"message":"Unable to process request",
						"data":{"text":""}
					}`)),
				}, nil
			}),
		},
		apiURL: "http://ocr.example/document/extract",
	}

	result, err := svc.ProcessOCR(context.Background(), &models.OCRRequest{
		ReqID:     "req-1",
		DocBase64: "valid-base64-ish",
	})
	if err != nil {
		t.Fatalf("ProcessOCR returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected structured OCR result")
	}
	if result.Success {
		t.Fatalf("expected failed OCR result, got %+v", result)
	}
	if result.UpstreamStatus != http.StatusInternalServerError {
		t.Fatalf("upstream status = %d, want 500", result.UpstreamStatus)
	}
	if result.Data.Blocks == nil {
		t.Fatal("expected normalized empty blocks slice")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
