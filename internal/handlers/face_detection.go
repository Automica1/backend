// internal/handlers/face_detection.go
package handlers

import (
	"context"
	"net/http"
	"time"
	"fmt"
	"strings"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	"chi-mongo-backend/internal/middleware"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type FaceDetectionHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
	faceAPIService services.FaceDetectionAPIService
	usageService   services.UsageService
	errorMapper    *apperrors.APIErrorMapper
}

func NewFaceDetectionHandler(creditsService services.CreditsService, userService services.UserService, faceAPIService services.FaceDetectionAPIService, usageService services.UsageService) *FaceDetectionHandler {
	return &FaceDetectionHandler{
		creditsService: creditsService,
		userService:    userService,
		faceAPIService: faceAPIService,
		usageService:   usageService,
		errorMapper:    apperrors.NewAPIErrorMapper(),
	}
}

func (h *FaceDetectionHandler) ProcessFaceDetection(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Get email from context (set by auth middleware)
	email, ok := r.Context().Value("email").(string)
	if !ok {
		// Track failed authentication
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      "unknown",
			Email:       "unknown",
			ServiceName: "face-detection",
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
	var req models.FaceDetectionRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		// Track validation failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      email,
			Email:       email,
			ServiceName: "face-detection",
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
			ServiceName: "face-detection",
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Try to get user by email, auto-create if not found
	user, err := h.userService.GetUserByEmail(ctx, email)
	if err != nil {
		// If user not found, try to auto-create them
		if apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
			// Auto-create user with email as user_id for Kinde users
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
					ServiceName: "face-detection",
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
				ServiceName: "face-detection",
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
			ServiceName: "face-detection",
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

	// Check if user has sufficient credits (at least 1)
	if balance.Credits < 1 {
		// Track insufficient credits
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "face-detection",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "insufficient credits for face detection operation",
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInsufficientCredits,
			http.StatusBadRequest,
			"insufficient credits for face detection operation (minimum 1 credit required)",
		))
		return
	}

	// Process face detection via external API
	faceResult, err := h.faceAPIService.ProcessFaceDetection(ctx, &req)
	if err != nil {
		// Track API failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "face-detection",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "face detection operation failed: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"Face detection operation failed: "+err.Error(),
		))
		return
	}

	fmt.Printf("Face Detection API Result: %+v\n", faceResult) // Debug log

	// Check if the API returned success
	if faceResult == nil || !faceResult.Success {
		// Still deduct credits for API usage even when detection fails
		deductReq := &models.DeductCreditsRequest{
			UserID: user.UserID,
			Amount: 1,
		}
		h.creditsService.DeductCredits(ctx, deductReq)

		// Track API failure (but still consider it a "successful" call since API responded)
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "face-detection",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     true, // API call succeeded even if detection failed
			ErrorMsg:    faceResult.Message,
			CreditsUsed: 1,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		// Create original response structure with specific field order
		originalResponse := struct {
			ReqID        string      `json:"req_id"`
			Success      bool        `json:"success"`
			ErrorMessage string      `json:"error_message"`
			Data         interface{} `json:"data"`
		}{
			ReqID:        faceResult.ReqID,
			Success:      faceResult.Success,
			ErrorMessage: faceResult.Message,
			Data:         faceResult.Data,
		}
		
		// Ensure data is empty array if nil
		if originalResponse.Data == nil {
			originalResponse.Data = []interface{}{}
		}
		
		// Handle error response based on authentication method
		if isAPIKeyAuth {
			// For API key authentication: return only original_response structure
			utils.SendJSONResponse(w, http.StatusBadRequest, originalResponse)
		} else {
			// For Bearer token (frontend): return full error with user-friendly message
			apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, faceResult.Message, originalResponse)
			utils.SendErrorResponse(w, apiError)
		}
		return
	}

	// API success: true - deduct 1 credit from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
		Amount: 1,
	}
	
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Track credit deduction failure
		h.trackUsage(r.Context(), &models.UsageTrackingRequest{
			UserID:      user.UserID,
			Email:       email,
			ServiceName: "face-detection",
			Endpoint:    r.URL.Path,
			Method:      r.Method,
			Success:     false,
			ErrorMsg:    "face detection completed but failed to deduct credits: " + err.Error(),
			CreditsUsed: 0,
			IPAddress:   h.getClientIP(r),
			UserAgent:   r.UserAgent(),
			AuthMethod:  h.getAuthMethod(r),
			ProcessTime: time.Since(startTime).Milliseconds(),
		})

		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"Face detection completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Track successful operation
	h.trackUsage(r.Context(), &models.UsageTrackingRequest{
		UserID:      user.UserID,
		Email:       email,
		ServiceName: "face-detection",
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

	// Send different responses based on authentication method
	if isAPIKeyAuth {
		// For API key authentication: return only faceResult
		utils.SendJSONResponse(w, http.StatusOK, faceResult)
	} else {
		// For Bearer token (frontend): return full response with credits info
		response := &models.FaceDetectionResponse{
			Message:          "Face detection completed successfully",
			UserID:           user.UserID,
			RemainingCredits: updatedBalance.Credits,
			FaceResult:       faceResult,
			ProcessedAt:      time.Now(),
		}
		utils.SendJSONResponse(w, http.StatusOK, response)
	}
}

// Helper methods for the FaceDetectionHandler
func (h *FaceDetectionHandler) trackUsage(ctx context.Context, req *models.UsageTrackingRequest) {
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

func (h *FaceDetectionHandler) getClientIP(r *http.Request) string {
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

func (h *FaceDetectionHandler) getAuthMethod(r *http.Request) string {
	if _, isAPIKeyAuth := middleware.GetAPIKeyFromContext(r.Context()); isAPIKeyAuth {
		return "api_key"
	}
	return "bearer_token"
}