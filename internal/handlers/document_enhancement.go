package handlers

import (
	"context"
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

type DocumentEnhancementHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
	enhanceAPIService services.DocumentEnhancementAPIService
	usageService   services.UsageService
	errorMapper    *apperrors.APIErrorMapper
}

func NewDocumentEnhancementHandler(
	creditsService services.CreditsService,
	userService services.UserService,
	enhanceAPIService services.DocumentEnhancementAPIService,
	usageService services.UsageService,
) *DocumentEnhancementHandler {
	return &DocumentEnhancementHandler{
		creditsService:    creditsService,
		userService:       userService,
		enhanceAPIService: enhanceAPIService,
		usageService:      usageService,
		errorMapper:       apperrors.NewAPIErrorMapper(),
	}
}

func (h *DocumentEnhancementHandler) ProcessDocumentEnhancement(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	billing, billingErr := ResolveServiceBilling(r, "document-enhance")
	if billingErr != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      "unknown",
			Email:       "unknown",
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    billingErr.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  middleware.ResolveAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, billingErr)
		return
	}
	email := billing.Email

	_, isAPIKeyAuth := middleware.GetAPIKeyFromContext(r.Context())

	var req models.DocumentEnhancementRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "request body parsing failed: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, err)
		return
	}

	if err := req.Validate(); err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "validation failed: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"validation failed: "+err.Error(),
		))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	userID, err := ResolveServiceUser(ctx, h.userService, billing)
	if err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  billing.AuthMethod,
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, err)
		return
	}

	balance, err := h.creditsService.GetBalance(ctx, userID)
	if err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "failed to check balance: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, err)
		return
	}

	if balance.Credits < 1 {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "insufficient credits for document enhancement operation",
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInsufficientCredits,
			http.StatusBadRequest,
			"insufficient credits for document enhancement operation (minimum 1 credit required)",
		))
		return
	}

	enhanceResult, err := h.enhanceAPIService.ProcessDocumentEnhancement(ctx, &req)
	if err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "document enhancement operation failed: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"document enhancement operation failed: "+err.Error(),
		))
		return
	}

	if enhanceResult == nil || !enhanceResult.Success {
		deductReq := &models.DeductCreditsRequest{
			UserID: userID,
			Amount: 1,
		}
		h.creditsService.DeductCredits(ctx, deductReq)

		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     true,
			ErrorMsg:    enhanceResult.Message,
			CreditsUsed: 1,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		originalResponse := struct {
			ReqID        string                 `json:"req_id"`
			Success      bool                   `json:"success"`
			ErrorMessage string                 `json:"error_message"`
			Result       string                 `json:"result"`
			Data         map[string]interface{} `json:"data,omitempty"`
		}{
			ReqID:        enhanceResult.ReqID,
			Success:      enhanceResult.Success,
			ErrorMessage: enhanceResult.Message,
			Result:       "",
		}
		if enhanceResult.Result != nil {
			originalResponse.Result = *enhanceResult.Result
		}
		if enhanceResult.EnhancementData != nil {
			originalResponse.Data = enhanceResult.EnhancementData
		}

		if isAPIKeyAuth {
			utils.SendJSONResponse(w, http.StatusBadRequest, originalResponse)
		} else {
			apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, enhanceResult.Message, originalResponse)
			utils.SendErrorResponse(w, apiError)
		}
		return
	}

	deductReq := &models.DeductCreditsRequest{
		UserID: userID,
		Amount: 1,
	}
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "document-enhancement",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "document enhancement completed but failed to deduct credits: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"document enhancement completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	h.trackUsage(r.Context(), &models.UsageTrackingRequest{
		UserID:      userID,
		Email:       email,
		ServiceName: "document-enhancement",
		Endpoint:    r.URL.Path,
		Method:      r.Method,
		Success:     true,
		ErrorMsg:    "",
		CreditsUsed: 1,
		IPAddress:   h.getClientIP(r),
		UserAgent:   r.UserAgent(),
		AuthMethod:  h.getAuthMethod(r),
		ProcessTime: time.Since(startTime).Milliseconds(),
	})

	if isAPIKeyAuth {
		utils.SendJSONResponse(w, http.StatusOK, enhanceResult)
	} else {
		response := &models.DocumentEnhancementResponse{
			Message:          "Document enhancement completed successfully",
			UserID:           userID,
			RemainingCredits: updatedBalance.Credits,
			EnhanceResult:    enhanceResult,
			ProcessedAt:      time.Now(),
		}
		utils.SendJSONResponse(w, http.StatusOK, response)
	}
}

func (h *DocumentEnhancementHandler) trackUsage(ctx context.Context, req *models.UsageTrackingRequest) {
	go func() {
		trackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.usageService.TrackUsage(trackCtx, req); err != nil {
			fmt.Printf("Failed to track usage: %v\n", err)
		}
	}()
}

func (h *DocumentEnhancementHandler) getClientIP(r *http.Request) string {
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

func (h *DocumentEnhancementHandler) getAuthMethod(r *http.Request) string {
	return middleware.ResolveAuthMethod(r)
}
