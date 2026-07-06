// internal/handlers/signature_verification.go
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

type SignatureVerificationHandler struct {
	creditsService       services.CreditsService
	userService          services.UserService
	signatureAPIService  services.SignatureVerificationAPIService
	betaKeyService       services.BetaKeyService
	betaServiceService   services.BetaServiceService
	betaFeedbackService  services.BetaFeedbackService
	usageService         services.UsageService
	errorMapper          *apperrors.APIErrorMapper
}

func NewSignatureVerificationHandler(
	creditsService services.CreditsService,
	userService services.UserService,
	signatureAPIService services.SignatureVerificationAPIService,
	betaKeyService services.BetaKeyService,
	betaServiceService services.BetaServiceService,
	betaFeedbackService services.BetaFeedbackService,
	usageService services.UsageService,
) *SignatureVerificationHandler {
	return &SignatureVerificationHandler{
		creditsService:      creditsService,
		userService:         userService,
		signatureAPIService: signatureAPIService,
		betaKeyService:      betaKeyService,
		betaServiceService:  betaServiceService,
		betaFeedbackService: betaFeedbackService,
		usageService:        usageService,
		errorMapper:         apperrors.NewAPIErrorMapper(),
	}
}

func (h *SignatureVerificationHandler) ProcessSignatureVerification(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	billing, billingErr := ResolveServiceBilling(r, "signature-verification")
	if billingErr != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      "unknown",
			Email:       "unknown",
			ServiceName: "signature-verification",
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

	// Check if request is authenticated via API key
	_, isAPIKeyAuth := middleware.GetAPIKeyFromContext(r.Context())

	// Parse request body
	var req models.SignatureVerificationRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		// Track validation failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "signature-verification",
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

	// Validate request
	if err := req.Validate(); err != nil {
		// Track validation failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "signature-verification",
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

	// Create context with timeout for the entire operation
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	userID, err := ResolveServiceUser(ctx, h.userService, billing)
	if err != nil {
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "signature-verification",
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

	// Check user's credit balance before processing
	balance, err := h.creditsService.GetBalance(ctx, userID)
	if err != nil {
		// Track balance check failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "signature-verification",
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

	// Check if user has sufficient credits (at least 2)
	if balance.Credits < 2 {
		// Track insufficient credits
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: "signature-verification",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "insufficient credits for signature verification operation",
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInsufficientCredits,
			http.StatusBadRequest,
			"insufficient credits for signature verification operation (minimum 2 credits required)",
		))
		return
	}

	betaKey := strings.TrimSpace(r.Header.Get("X-Beta-Key"))
	if betaKey == "" {
		betaKey = strings.TrimSpace(req.BetaKey)
	}

	useBeta := betaKey != ""
	usageServiceName := h.signatureUsageServiceName(useBeta)
	serviceName := "signature-verification"
	var betaKeyRecord *models.BetaKey
	var betaServiceURL string
	var betaServiceTag string
	if useBeta {
		record, err := h.betaKeyService.ValidateKey(ctx, serviceName, betaKey, email)
		if err != nil {
			h.trackUsage(r.Context(), &models.UsageTrackingRequest{
				UserID:      userID,
				Email:       email,
				ServiceName: usageServiceName,
				Endpoint:    r.URL.Path,
				Method:      r.Method,
				Success:     false,
				ErrorMsg:    "beta key validation failed: " + err.Error(),
				CreditsUsed: 0,
				IPAddress:   h.getClientIP(r),
				UserAgent:   r.UserAgent(),
				AuthMethod:  h.getAuthMethod(r),
				ProcessTime: time.Since(startTime).Milliseconds(),
			})

			utils.SendErrorResponse(w, err)
			return
		}
		betaKeyRecord = record
		betaServiceTag = strings.TrimSpace(record.BetaServiceTag)
		if betaServiceTag == "" {
			utils.SendErrorResponse(w, apperrors.NewAppError(
				apperrors.ErrForbidden,
				http.StatusForbidden,
				"beta key is not configured for routing",
			))
			return
		}

		betaService, svcErr := h.betaServiceService.GetActiveByTag(ctx, betaServiceTag)
		if svcErr != nil {
			h.trackUsage(r.Context(), &models.UsageTrackingRequest{
				UserID:         userID,
				Email:          email,
				ServiceName:    usageServiceName,
				Endpoint:       r.URL.Path,
				Method:         r.Method,
				Success:        false,
				ErrorMsg:       "beta service lookup failed: " + svcErr.Error(),
				CreditsUsed:    0,
				IPAddress:      h.getClientIP(r),
				UserAgent:      r.UserAgent(),
				AuthMethod:     h.getAuthMethod(r),
				ProcessTime:    time.Since(startTime).Milliseconds(),
				BetaServiceTag: betaServiceTag,
			})
			utils.SendErrorResponse(w, svcErr)
			return
		}
		if betaService.ServiceName != serviceName {
			utils.SendErrorResponse(w, apperrors.NewAppError(
				apperrors.ErrForbidden,
				http.StatusForbidden,
				"beta service is not valid for this endpoint",
			))
			return
		}
		betaServiceURL = betaService.APIURL

		if h.betaFeedbackService != nil {
			hasPending, pendingErr := h.betaFeedbackService.HasPendingSession(ctx, userID, serviceName)
			if pendingErr != nil {
				utils.SendErrorResponse(w, pendingErr)
				return
			}
			if hasPending && balance.Credits < 2 {
				h.trackUsage(r.Context(), &models.UsageTrackingRequest{
					UserID:      userID,
					Email:       email,
					ServiceName: usageServiceName,
					Endpoint:    r.URL.Path,
					Method:      r.Method,
					Success:     false,
					ErrorMsg:    "pending beta feedback must be submitted before another beta run",
					CreditsUsed: 0,
					IPAddress:   h.getClientIP(r),
					UserAgent:   r.UserAgent(),
					AuthMethod:  h.getAuthMethod(r),
					ProcessTime: time.Since(startTime).Milliseconds(),
				})

				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrValidation,
					http.StatusBadRequest,
					"Please submit expected results for your last custom model test before running another beta request",
				))
				return
			}
		}
	}

	// Process signature verification via external API
	verificationResult, err := h.signatureAPIService.ProcessSignatureVerification(ctx, &req, &services.SignatureVerificationProcessOptions{
		UseBeta:        useBeta,
		BetaServiceURL: betaServiceURL,
	})
	if err != nil {
		// Track API failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: usageServiceName,
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "signature verification operation failed: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		statusCode := http.StatusInternalServerError
		if useBeta && strings.Contains(err.Error(), "not configured") {
			statusCode = http.StatusServiceUnavailable
		}

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			statusCode,
			"signature verification operation failed: "+err.Error(),
		))
		return
	}

	fmt.Printf("Signature Verification API Result: %+v\n", verificationResult) // Debug log

	// Check if the API returned success
	if verificationResult == nil || !verificationResult.Success {
		// Still deduct credits for API usage even when verification fails
		deductReq := &models.DeductCreditsRequest{
			UserID: userID,
			Amount: 2,
		}
		updatedBalance, _ := h.creditsService.DeductCredits(ctx, deductReq)
		remainingCredits := 0
		if updatedBalance != nil {
			remainingCredits = updatedBalance.Credits
		}

		var failureMessage string
		if verificationResult != nil {
			failureMessage = verificationResult.Message
		}

		// Track API failure (but still consider it a "successful" call since API responded)
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: usageServiceName,
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     true, // API call succeeded even if verification failed
			ErrorMsg:    failureMessage,
			CreditsUsed: 2,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		actualResult := h.betaFeedbackActualResult(verificationResult, models.BetaFeedbackRunOutcomeFailed)
		betaFeedbackSessionID := h.createBetaFeedbackSessionIfNeeded(
			ctx,
			useBeta,
			&models.User{UserID: userID},
			email,
			serviceName,
			betaKeyRecord,
			req.DocBase64,
			h.verificationReqID(verificationResult),
			actualResult,
			models.BetaFeedbackRunOutcomeFailed,
			failureMessage,
			2,
		)

		// Create original response structure
		var originalResponse struct {
			ReqID        string                 `json:"req_id"`
			Success      bool                   `json:"success"`
			ErrorMessage string                 `json:"error_message"`
			Data         map[string]interface{} `json:"data"`
		}
		if verificationResult != nil {
			originalResponse.ReqID = verificationResult.ReqID
			originalResponse.Success = verificationResult.Success
			originalResponse.ErrorMessage = verificationResult.Message
		}
		originalResponse.Data = map[string]interface{}{}

		if isAPIKeyAuth {
			apiResponse := map[string]interface{}{
				"req_id":                   originalResponse.ReqID,
				"success":                  originalResponse.Success,
				"error_message":            originalResponse.ErrorMessage,
				"data":                     originalResponse.Data,
				"remaining_credits":        remainingCredits,
				"beta_feedback_session_id": betaFeedbackSessionID,
				"beta_feedback_pending":    betaFeedbackSessionID != "",
			}
			utils.SendJSONResponse(w, http.StatusBadRequest, apiResponse)
		} else {
			apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, failureMessage, originalResponse)
			if betaFeedbackSessionID != "" {
				apiError.WithBetaFeedback(betaFeedbackSessionID, remainingCredits)
			}
			utils.SendErrorResponse(w, apiError)
		}
		return
	}

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: userID,
		Amount: 2,
	}

	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Track credit deduction failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      userID,
			Email:       email,
			ServiceName: usageServiceName,
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "signature verification completed but failed to deduct credits: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"signature verification completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Track successful operation
	h.trackUsage(r.Context(), &models.UsageTrackingRequest{
		UserID:      userID,
		Email:       email,
		ServiceName: usageServiceName,
		Endpoint:    r.URL.Path,
		Method:      r.Method,
		Success:     true,
		ErrorMsg:    "",
		CreditsUsed: 2, // Fixed: should be 2, not 1
		IPAddress:   h.getClientIP(r),
		UserAgent:   r.UserAgent(),
		AuthMethod:  h.getAuthMethod(r),
		ProcessTime: time.Since(startTime).Milliseconds(),
	})

	var betaFeedbackSessionID string
	if useBeta && h.betaFeedbackService != nil {
		actualResult := h.betaFeedbackActualResult(verificationResult, models.BetaFeedbackRunOutcomeCompleted)
		betaFeedbackSessionID = h.createBetaFeedbackSessionIfNeeded(
			ctx,
			useBeta,
			&models.User{UserID: userID},
			email,
			serviceName,
			betaKeyRecord,
			req.DocBase64,
			verificationResult.ReqID,
			actualResult,
			models.BetaFeedbackRunOutcomeCompleted,
			"",
			2,
		)
	}

	// Send different responses based on authentication method
	if isAPIKeyAuth {
		apiResponse := map[string]interface{}{
			"req_id":                   verificationResult.ReqID,
			"success":                  verificationResult.Success,
			"status":                   verificationResult.Status,
			"message":                  verificationResult.Message,
			"data":                     verificationResult.Data,
			"remaining_credits":        updatedBalance.Credits,
			"beta_feedback_session_id": betaFeedbackSessionID,
			"beta_feedback_pending":    betaFeedbackSessionID != "",
		}
		utils.SendJSONResponse(w, http.StatusOK, apiResponse)
	} else {
		response := &models.SignatureVerificationResponse{
			Message:               "Signature verification completed successfully",
			UserID:                userID,
			RemainingCredits:      updatedBalance.Credits,
			VerificationResult:    verificationResult,
			ProcessedAt:           time.Now(),
			BetaFeedbackSessionID: betaFeedbackSessionID,
			BetaFeedbackPending:   betaFeedbackSessionID != "",
		}
		utils.SendJSONResponse(w, http.StatusOK, response)
	}
}

// Helper methods for the SignatureVerificationHandler
func (h *SignatureVerificationHandler) trackUsage(ctx context.Context, req *models.UsageTrackingRequest) {
	// Track usage asynchronously to not block the response
	go func() {
		trackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.usageService.TrackUsage(trackCtx, req); err != nil {
			// Log error but don't fail the request
			// In production, use proper logging
			fmt.Printf("Failed to track usage: %v\n", err)
		}
	}()
}

func (h *SignatureVerificationHandler) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, get the first one
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

func (h *SignatureVerificationHandler) getAuthMethod(r *http.Request) string {
	return middleware.ResolveAuthMethod(r)
}

func (h *SignatureVerificationHandler) signatureUsageServiceName(useBeta bool) string {
	if useBeta {
		return "signature-verification-beta"
	}
	return "signature-verification"
}

// createBetaFeedbackSessionIfNeeded stores a feedback session after a charged beta run.
// To enable beta feedback for a new service: add it to SupportedBetaServices, wire beta key
// validation in that service's handler, and call this helper after credit deduction.
func (h *SignatureVerificationHandler) createBetaFeedbackSessionIfNeeded(
	ctx context.Context,
	useBeta bool,
	user *models.User,
	email string,
	serviceName string,
	betaKeyRecord *models.BetaKey,
	inputs []string,
	reqID string,
	actualResult *models.BetaFeedbackActualResult,
	runOutcome string,
	failureMessage string,
	creditsCharged int,
) string {
	if !useBeta || h.betaFeedbackService == nil {
		return ""
	}

	betaKeyPrefix := ""
	betaServiceTag := ""
	if betaKeyRecord != nil {
		betaKeyPrefix = betaKeyRecord.KeyPrefix
		betaServiceTag = betaKeyRecord.BetaServiceTag
	}

	session, err := h.betaFeedbackService.CreateSession(ctx, &models.CreateBetaFeedbackSessionRequest{
		UserID:         user.UserID,
		Email:          email,
		ServiceName:    serviceName,
		BetaKeyPrefix:  betaKeyPrefix,
		BetaServiceTag: betaServiceTag,
		ReqID:          reqID,
		Inputs:         inputs,
		ActualResult:   actualResult,
		CreditsCharged: creditsCharged,
		RunOutcome:     runOutcome,
		FailureMessage: failureMessage,
	})
	if err != nil {
		fmt.Printf("Failed to create beta feedback session: %v\n", err)
		return ""
	}
	if session != nil {
		return session.ID.Hex()
	}
	return ""
}

func (h *SignatureVerificationHandler) verificationReqID(result *models.SignatureVerificationResult) string {
	if result == nil {
		return ""
	}
	return result.ReqID
}

func (h *SignatureVerificationHandler) betaFeedbackActualResult(
	result *models.SignatureVerificationResult,
	runOutcome string,
) *models.BetaFeedbackActualResult {
	if result != nil && result.Data != nil {
		return &models.BetaFeedbackActualResult{
			SimilarityPercentage: result.Data.SimilarityPercentage,
			Classification:       result.Data.Classification,
		}
	}
	if runOutcome == models.BetaFeedbackRunOutcomeFailed {
		return &models.BetaFeedbackActualResult{Classification: "Not-Detected"}
	}
	return nil
}