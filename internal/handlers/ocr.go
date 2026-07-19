package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

const ocrCreditCost = 4

type OCRHandler struct {
	creditsService     services.CreditsService
	userService        services.UserService
	ocrAPIService      services.OCRAPIService
	gpuPoolService     services.GPUPoolService
	usageService       services.UsageService
	betaServiceService services.BetaServiceService
}

func NewOCRHandler(
	creditsService services.CreditsService,
	userService services.UserService,
	ocrAPIService services.OCRAPIService,
	gpuPoolService services.GPUPoolService,
	usageService services.UsageService,
	betaServiceService services.BetaServiceService,
) *OCRHandler {
	return &OCRHandler{
		creditsService:     creditsService,
		userService:        userService,
		ocrAPIService:      ocrAPIService,
		gpuPoolService:     gpuPoolService,
		usageService:       usageService,
		betaServiceService: betaServiceService,
	}
}

func (h *OCRHandler) ProcessOCR(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	billing, billingErr := ResolveServiceBilling(r, models.OCRServiceName)
	if billingErr != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, "unknown", "unknown", false, billingErr.Error(), 0, startTime))
		utils.SendErrorResponse(w, billingErr)
		return
	}
	email := billing.Email
	_, isAPIKeyAuth := middleware.GetAPIKeyFromContext(r.Context())
	policy := h.resolveOCRPolicy(r.Context())

	var req models.OCRRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, email, email, false, "request body parsing failed: "+err.Error(), 0, startTime))
		utils.SendErrorResponse(w, err)
		return
	}

	if err := req.ValidateWithPolicy(policy); err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, email, email, false, "validation failed: "+err.Error(), 0, startTime))
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"validation failed: "+err.Error(),
		))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), services.OCRAPIRequestTimeout())
	defer cancel()

	userID, err := ResolveServiceUser(ctx, h.userService, billing)
	if err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, email, email, false, err.Error(), 0, startTime))
		utils.SendErrorResponse(w, err)
		return
	}

	status, err := h.gpuPoolService.GetStatus(ctx, userID, models.OCRGPUServiceTag)
	if err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, "failed to check OCR GPU session: "+err.Error(), 0, startTime))
		utils.SendErrorResponse(w, err)
		return
	}
	if status.State != models.GPUPoolStateReady || !status.UserActive {
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, "OCR GPU session is not ready", 0, startTime))
		utils.SendJSONResponse(w, http.StatusConflict, models.OCRGPURequiredResponse{
			Success:    false,
			Status:     "gpu_session_required",
			Message:    "Start an OCR GPU session before running OCR.",
			ServiceTag: models.OCRGPUServiceTag,
			State:      status.State,
			UserActive: status.UserActive,
			PollURL:    status.PollURL,
		})
		return
	}

	balance, err := h.creditsService.GetBalance(ctx, userID)
	if err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, "failed to check balance: "+err.Error(), 0, startTime))
		utils.SendErrorResponse(w, err)
		return
	}
	ocrHitCost := resolveOCRHitCost(policy)
	if balance.Credits < ocrHitCost {
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, "insufficient credits for OCR operation", 0, startTime))
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInsufficientCredits,
			http.StatusBadRequest,
			fmt.Sprintf("insufficient credits for OCR operation (minimum %d credits required)", ocrHitCost),
		))
		return
	}

	ocrResult, err := h.ocrAPIService.ProcessOCR(ctx, &req)
	if err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, "OCR operation failed: "+err.Error(), 0, startTime))
		statusCode := http.StatusBadGateway
		message := "OCR operation failed: " + err.Error()
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			statusCode = http.StatusGatewayTimeout
			message = "OCR operation timed out"
		}
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			statusCode,
			message,
		))
		return
	}
	if ocrResult == nil || !ocrResult.Success {
		ocrResult = normalizeOCRFailureResult(&req, ocrResult)
		msg := ocrResult.Message
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, msg, 0, startTime))
		utils.SendJSONResponse(w, ocrFailureStatusCode(ocrResult), ocrResult)
		return
	}

	updatedBalance, err := h.creditsService.DeductCredits(ctx, &models.DeductCreditsRequest{
		UserID: userID,
		Amount: ocrHitCost,
	})
	if err != nil {
		h.trackUsage(r.Context(), h.usageRequest(r, userID, email, false, "OCR completed but failed to deduct credits: "+err.Error(), 0, startTime))
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"OCR completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	h.trackUsage(r.Context(), h.usageRequest(r, userID, email, true, "", ocrHitCost, startTime))

	if isAPIKeyAuth {
		utils.SendJSONResponse(w, http.StatusOK, ocrResult)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, &models.OCRResponse{
		Message:          "OCR completed successfully",
		UserID:           userID,
		RemainingCredits: updatedBalance.Credits,
		OCRResult:        ocrResult,
		ProcessedAt:      time.Now(),
	})
}

func (h *OCRHandler) resolveOCRPolicy(ctx context.Context) *models.ServicePolicy {
	if h == nil || h.betaServiceService == nil {
		return nil
	}
	service, err := h.betaServiceService.GetActiveByServiceName(ctx, models.OCRServiceName)
	if err != nil || service == nil {
		return nil
	}
	return service.ServicePolicy
}

func resolveOCRHitCost(policy *models.ServicePolicy) int {
	if policy != nil && policy.Pricing != nil && policy.Pricing.CreditsPerHit != nil {
		return *policy.Pricing.CreditsPerHit
	}
	return ocrCreditCost
}

func (h *OCRHandler) usageRequest(r *http.Request, userID, email string, success bool, errMsg string, creditsUsed int, start time.Time) *models.UsageTrackingRequest {
	return &models.UsageTrackingRequest{
		UserID:      userID,
		Email:       email,
		ServiceName: models.OCRServiceName,
		Endpoint:    r.URL.Path,
		Method:      r.Method,
		Success:     success,
		ErrorMsg:    errMsg,
		CreditsUsed: creditsUsed,
		IPAddress:   h.getClientIP(r),
		UserAgent:   r.UserAgent(),
		AuthMethod:  middleware.ResolveAuthMethod(r),
		ProcessTime: time.Since(start).Milliseconds(),
	}
}

func normalizeOCRFailureResult(req *models.OCRRequest, result *models.OCRResult) *models.OCRResult {
	if result == nil {
		return &models.OCRResult{
			ReqID:   req.ReqID,
			Success: false,
			Status:  "failed",
			Message: "OCR operation failed",
			Data: models.OCRData{
				Text:   "",
				Blocks: []map[string]interface{}{},
			},
		}
	}
	if result.ReqID == "" {
		result.ReqID = req.ReqID
	}
	if result.Status == "" {
		result.Status = "failed"
	}
	if result.Message == "" {
		result.Message = "OCR operation failed"
	}
	if result.Data.Blocks == nil {
		result.Data.Blocks = []map[string]interface{}{}
	}
	return result
}

func ocrFailureStatusCode(result *models.OCRResult) int {
	if result.UpstreamStatus >= http.StatusInternalServerError {
		return http.StatusBadGateway
	}
	return http.StatusBadRequest
}

func (h *OCRHandler) trackUsage(ctx context.Context, req *models.UsageTrackingRequest) {
	go func() {
		trackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.usageService.TrackUsage(trackCtx, req); err != nil {
			fmt.Printf("Failed to track OCR usage: %v\n", err)
		}
	}()
}

func (h *OCRHandler) getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}
