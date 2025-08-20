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
	creditsService      services.CreditsService
	userService         services.UserService
	signatureAPIService services.SignatureVerificationAPIService
	usageService        services.UsageService
	errorMapper         *apperrors.APIErrorMapper
}

func NewSignatureVerificationHandler(
	creditsService services.CreditsService,
	userService services.UserService,
	signatureAPIService services.SignatureVerificationAPIService,
	usageService services.UsageService,
) *SignatureVerificationHandler {
	return &SignatureVerificationHandler{
		creditsService:      creditsService,
		userService:         userService,
		signatureAPIService: signatureAPIService,
		usageService:        usageService,
		errorMapper:         apperrors.NewAPIErrorMapper(),
	}
}

func (h *SignatureVerificationHandler) ProcessSignatureVerification(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Get email from context (set by auth middleware)
	email, ok := r.Context().Value("email").(string)
	if !ok {
		// Track failed authentication
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      "unknown",
			Email:       "unknown",
			ServiceName: "signature-verification",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "email not found in context",
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

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

	// Try to get user by email, auto-create if not found
	user, err := h.userService.GetUserByEmail(ctx, email)
	if err != nil {
		// If user not found, try to auto-create them
		if apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
			registerReq := &models.RegisterUserRequest{
				UserID: email, // Use email as user_id for Kinde users
				Email:  email,
			}

			createdUser, createErr := h.userService.RegisterUser(ctx, registerReq)
			if createErr != nil {
				// Track user creation failure
				h.trackUsage(r.Context(), &models.UsageTrackingRequest{
					UserID:      email,
					Email:       email,
					ServiceName: "signature-verification",
					Endpoint:    r.URL.Path,
					Method:      r.Method,
					Success:     false,
					ErrorMsg:    "failed to auto-create user: " + createErr.Error(),
					CreditsUsed: 0,
					IPAddress:   h.getClientIP(r),
					UserAgent:   r.UserAgent(),
					AuthMethod:  h.getAuthMethod(r),
					ProcessTime: time.Since(startTime).Milliseconds(),
				})

				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to auto-create user: "+createErr.Error(),
				))
				return
			}
			user = &createdUser.User
		} else {
			// Track user lookup failure
			h.trackUsage(r.Context(), &models.UsageTrackingRequest{
				UserID:      email,
				Email:       email,
				ServiceName: "signature-verification",
				Endpoint:    r.URL.Path,
				Method:      r.Method,
				Success:     false,
				ErrorMsg:    "user not found: " + err.Error(),
				CreditsUsed: 0,
				IPAddress:   h.getClientIP(r),
				UserAgent:   r.UserAgent(),
				AuthMethod:  h.getAuthMethod(r),
				ProcessTime: time.Since(startTime).Milliseconds(),
			})

			utils.SendErrorResponse(w, apperrors.NewAppError(
				apperrors.ErrUserNotFound,
				http.StatusNotFound,
				"user not found: "+err.Error(),
			))
			return
		}
	}

	// Check user's credit balance before processing
	balance, err := h.creditsService.GetBalance(ctx, user.UserID)
	if err != nil {
		// Track balance check failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
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
			UserID:      user.UserID,
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

	// Process signature verification via external API
	verificationResult, err := h.signatureAPIService.ProcessSignatureVerification(ctx, &req)
	if err != nil {
		// Track API failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "signature-verification",
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

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"signature verification operation failed: "+err.Error(),
		))
		return
	}

	fmt.Printf("Signature Verification API Result: %+v\n", verificationResult) // Debug log

	// Check if the API returned success
	if verificationResult == nil || !verificationResult.Success {
		// Still deduct credits for API usage even when verification fails
		deductReq := &models.DeductCreditsRequest{
			UserID: user.UserID,
			Amount: 2,
		}
		h.creditsService.DeductCredits(ctx, deductReq)

		// Track API failure (but still consider it a "successful" call since API responded)
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "signature-verification",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     true, // API call succeeded even if verification failed
			ErrorMsg:    verificationResult.Message,
			CreditsUsed: 2,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		// Create original response structure
		originalResponse := struct {
			ReqID        string                 `json:"req_id"`
			Success      bool                   `json:"success"`
			ErrorMessage string                 `json:"error_message"`
			Data         map[string]interface{} `json:"data"`
		}{
			ReqID:        verificationResult.ReqID,
			Success:      verificationResult.Success,
			ErrorMessage: verificationResult.Message,
			Data:         map[string]interface{}{},
		}

		if isAPIKeyAuth {
			utils.SendJSONResponse(w, http.StatusBadRequest, originalResponse)
		} else {
			apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, verificationResult.Message, originalResponse)
			utils.SendErrorResponse(w, apiError)
		}
		return
	}

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
		Amount: 2,
	}

	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Track credit deduction failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "signature-verification",
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
		UserID:      user.UserID,
		Email:       email,
		ServiceName: "signature-verification",
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

	// Send different responses based on authentication method
	if isAPIKeyAuth {
		utils.SendJSONResponse(w, http.StatusOK, verificationResult)
	} else {
		response := &models.SignatureVerificationResponse{
			Message:            "Signature verification completed successfully",
			UserID:             user.UserID,
			RemainingCredits:   updatedBalance.Credits,
			VerificationResult: verificationResult,
			ProcessedAt:        time.Now(),
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
	if _, isAPIKeyAuth := middleware.GetAPIKeyFromContext(r.Context()); isAPIKeyAuth {
		return "api_key"
	}
	return "bearer_token"
}