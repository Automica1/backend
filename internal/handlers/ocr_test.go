package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
)

func TestProcessOCRDoesNotDeductCreditsOnFailedOCR(t *testing.T) {
	credits := &ocrTestCreditsService{balance: 10}
	handler := NewOCRHandler(
		credits,
		nil,
		&ocrTestAPIService{result: &models.OCRResult{
			ReqID:   "req-1",
			Success: false,
			Status:  "failed",
			Message: "Unable to process request",
			Data: models.OCRData{
				Text:   "",
				Blocks: []map[string]interface{}{},
			},
		}},
		&ocrTestGPUPoolService{},
		&ocrTestUsageService{},
		&ocrTestBetaServiceService{},
	)

	body := bytes.NewBufferString(`{"req_id":"req-1","doc_base64":"valid-base64-ish"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ocr", body)
	req = req.WithContext(context.WithValue(req.Context(), middleware.GuestPassContextKey, &models.GuestPass{
		WalletUserID: "wallet-1",
		IsActive:     true,
	}))
	rec := httptest.NewRecorder()

	handler.ProcessOCR(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if credits.deductCalls != 0 {
		t.Fatalf("DeductCredits calls = %d, want 0", credits.deductCalls)
	}

	var result models.OCRResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Success || result.Status != "failed" || result.ReqID != "req-1" {
		t.Fatalf("unexpected OCR failure response: %+v", result)
	}
	if result.Data.Blocks == nil {
		t.Fatal("expected blocks to be an empty array, got nil")
	}
}

func TestProcessOCRUsesServicePolicyCredits(t *testing.T) {
	credits := &ocrTestCreditsService{balance: 10}
	hitCredits := 2
	handler := NewOCRHandler(
		credits,
		nil,
		&ocrTestAPIService{result: &models.OCRResult{
			ReqID:   "req-2",
			Success: true,
			Status:  "completed",
			Message: "done",
			Data: models.OCRData{
				Text:   "hello",
				Blocks: []map[string]interface{}{},
			},
		}},
		&ocrTestGPUPoolService{},
		&ocrTestUsageService{},
		&ocrTestBetaServiceService{
			service: &models.BetaService{
				ServiceName: models.OCRServiceName,
				ServicePolicy: &models.ServicePolicy{
					Pricing: &models.ServicePolicyPricing{CreditsPerHit: &hitCredits},
				},
			},
		},
	)

	body := bytes.NewBufferString(`{"req_id":"req-2","doc_base64":"valid-base64-ish"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ocr", body)
	req = req.WithContext(context.WithValue(req.Context(), middleware.GuestPassContextKey, &models.GuestPass{
		WalletUserID: "wallet-1",
		IsActive:     true,
	}))
	rec := httptest.NewRecorder()

	handler.ProcessOCR(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if credits.lastDeductAmount != 2 {
		t.Fatalf("deduct amount = %d, want 2", credits.lastDeductAmount)
	}
}

func TestProcessOCRRejectsDocumentOverConfiguredUploadLimit(t *testing.T) {
	credits := &ocrTestCreditsService{balance: 10}
	maxUpload := 1
	handler := NewOCRHandler(
		credits,
		nil,
		&ocrTestAPIService{result: &models.OCRResult{
			ReqID:   "req-3",
			Success: true,
			Status:  "completed",
			Message: "done",
			Data: models.OCRData{
				Text:   "hello",
				Blocks: []map[string]interface{}{},
			},
		}},
		&ocrTestGPUPoolService{},
		&ocrTestUsageService{},
		&ocrTestBetaServiceService{
			service: &models.BetaService{
				ServiceName: models.OCRServiceName,
				ServicePolicy: &models.ServicePolicy{
					Limits: &models.ServicePolicyLimits{MaxUploadSizeMB: &maxUpload},
				},
			},
		},
	)

	body := bytes.NewBufferString(`{"req_id":"req-3","doc_base64":"data:application/pdf;base64,` + bigBase64Payload() + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ocr", body)
	req = req.WithContext(context.WithValue(req.Context(), middleware.GuestPassContextKey, &models.GuestPass{
		WalletUserID: "wallet-1",
		IsActive:     true,
	}))
	rec := httptest.NewRecorder()

	handler.ProcessOCR(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if credits.deductCalls != 0 {
		t.Fatalf("DeductCredits calls = %d, want 0", credits.deductCalls)
	}
}

func bigBase64Payload() string {
	return strings.Repeat("QUFB", 400000)
}

type ocrTestCreditsService struct {
	balance          int
	deductCalls      int
	lastDeductAmount int
}

func (s *ocrTestCreditsService) GetBalance(ctx context.Context, userID string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: userID, Credits: s.balance}, nil
}

func (s *ocrTestCreditsService) GetBalanceByEmail(ctx context.Context, email string) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: email, Credits: s.balance}, nil
}

func (s *ocrTestCreditsService) AddCredits(ctx context.Context, req *models.AddCreditsRequest) (*models.CreditsResponse, error) {
	return &models.CreditsResponse{UserID: req.UserID, Credits: s.balance + req.Amount}, nil
}

func (s *ocrTestCreditsService) DeductCredits(ctx context.Context, req *models.DeductCreditsRequest) (*models.CreditsResponse, error) {
	s.deductCalls++
	s.lastDeductAmount = req.Amount
	return &models.CreditsResponse{UserID: req.UserID, Credits: s.balance - req.Amount}, nil
}

type ocrTestAPIService struct {
	result *models.OCRResult
	err    error
}

func (s *ocrTestAPIService) ProcessOCR(ctx context.Context, req *models.OCRRequest) (*models.OCRResult, error) {
	return s.result, s.err
}

type ocrTestGPUPoolService struct{}

func (s *ocrTestGPUPoolService) Start(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) Stop(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) GetStatus(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	return &models.GPUPoolStatusResponse{
		ServiceTag: serviceTag,
		State:      models.GPUPoolStateReady,
		UserActive: true,
	}, nil
}

func (s *ocrTestGPUPoolService) ListAdmin(ctx context.Context) ([]*models.GPUPool, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminWarmStart(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminShutdown(ctx context.Context, serviceTag string, immediate bool) (*models.GPUPool, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminCancelGrace(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminExtendGrace(ctx context.Context, serviceTag string, extend time.Duration) (*models.GPUPool, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminAbortProvision(ctx context.Context, serviceTag string) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminRetryProvision(ctx context.Context, serviceTag string) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) AdminRecover(ctx context.Context, serviceTag string) (*models.GPUPoolRecoveryReport, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) NodeClaimedByOtherPool(ctx context.Context, serviceTag, nodeID, publicIP string) (*models.GPUPool, error) {
	return nil, nil
}

func (s *ocrTestGPUPoolService) ListInventory(ctx context.Context, serviceTag, providerOverride string) (*models.GPUPoolInventory, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) ReconcileIdleWarmPools(ctx context.Context) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) ReconcileOrphanPools(ctx context.Context) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) ReconcileScheduledState(ctx context.Context) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) HandleMeterTick(ctx context.Context, serviceTag, userID string) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) HandleProvisionFailed(ctx context.Context, serviceTag string) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) OnProvisionSkippedNoSessions(ctx context.Context, serviceTag string) error {
	return errors.New("not implemented")
}

func (s *ocrTestGPUPoolService) HandleProvisionNoSessions(ctx context.Context, serviceTag string, afterPipeline bool) (bool, error) {
	return false, errors.New("not implemented")
}

type ocrTestUsageService struct{}

func (s *ocrTestUsageService) TrackUsage(ctx context.Context, req *models.UsageTrackingRequest) error {
	return nil
}

func (s *ocrTestUsageService) GetGlobalStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error) {
	return nil, nil
}

func (s *ocrTestUsageService) GetUserStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UserUsageStats, error) {
	return nil, nil
}

func (s *ocrTestUsageService) GetServiceUserStats(ctx context.Context, serviceName string, startDate, endDate *time.Time) ([]models.ServiceUserStats, error) {
	return nil, nil
}

func (s *ocrTestUsageService) GetUserUsageHistory(ctx context.Context, userID string, startDate, endDate *time.Time, limit, skip int) ([]models.ServiceUsage, error) {
	return nil, nil
}

func (s *ocrTestUsageService) GetServiceUsageHistory(ctx context.Context, serviceName string, startDate, endDate *time.Time, limit, skip int) ([]models.ServiceUsage, error) {
	return nil, nil
}

func (s *ocrTestUsageService) GetAllUsageHistory(ctx context.Context, startDate, endDate *time.Time, limit, skip int) ([]models.ServiceUsage, error) {
	return nil, nil
}

func (s *ocrTestUsageService) CountUsageHistory(ctx context.Context, startDate, endDate *time.Time) (int64, error) {
	return 0, nil
}

func (s *ocrTestUsageService) CountServiceUsageHistory(ctx context.Context, serviceName string, startDate, endDate *time.Time) (int64, error) {
	return 0, nil
}

type ocrTestBetaServiceService struct {
	service *models.BetaService
}

func (s *ocrTestBetaServiceService) Create(ctx context.Context, req *models.CreateBetaServiceRequest) (*models.BetaService, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestBetaServiceService) GetByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestBetaServiceService) GetActiveByServiceName(ctx context.Context, serviceName string) (*models.BetaService, error) {
	if s.service != nil && s.service.ServiceName == serviceName {
		return s.service, nil
	}
	return nil, errors.New("not found")
}

func (s *ocrTestBetaServiceService) GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestBetaServiceService) List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error) {
	return nil, errors.New("not implemented")
}

func (s *ocrTestBetaServiceService) Update(ctx context.Context, tag string, req *models.UpdateBetaServiceRequest) (*models.BetaService, error) {
	return nil, errors.New("not implemented")
}
