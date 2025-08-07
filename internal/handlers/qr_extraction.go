// internal/handlers/qr_extraction.go
package handlers

import (
	"context"
	"net/http"
	"time"
	"fmt"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	"chi-mongo-backend/internal/middleware"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type QRExtractionHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
	qrAPIService   services.QRExtractionAPIService
	errorMapper    *apperrors.APIErrorMapper
}

func NewQRExtractionHandler(creditsService services.CreditsService, userService services.UserService, qrAPIService services.QRExtractionAPIService) *QRExtractionHandler {
	return &QRExtractionHandler{
		creditsService: creditsService,
		userService:    userService,
		qrAPIService:   qrAPIService,
		errorMapper:    apperrors.NewAPIErrorMapper(),
	}
}

func (h *QRExtractionHandler) ProcessQRExtraction(w http.ResponseWriter, r *http.Request) {
	// Get email from context (set by auth middleware)
	email, ok := r.Context().Value("email").(string)
	if !ok {
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
	var req models.QRExtractionRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
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
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to auto-create user: "+createErr.Error(),
				))
				return
			}
			user = &createdUser.User
		} else {
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
		utils.SendErrorResponse(w, err)
		return
	}

	// Check if user has sufficient credits (at least 2)
	if balance.Credits < 2 {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInsufficientCredits,
			http.StatusBadRequest,
			"insufficient credits for QR extraction operation (minimum 2 credits required)",
		))
		return
	}

	// Process QR extraction via external API
	qrResult, err := h.qrAPIService.ProcessQRExtraction(ctx, &req)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"QR extraction operation failed: "+err.Error(),
		))
		return
	}

	fmt.Printf("QR Extraction API Result: %+v\n", qrResult) // Debug log

	// Check if the API returned success
	if qrResult == nil || !qrResult.Success {
		// Create original response structure with specific field order
		originalResponse := struct {
			ReqID        string      `json:"req_id"`
			Success      bool        `json:"success"`
			ErrorMessage string      `json:"error_message"`
			Result       interface{} `json:"result"`
		}{
			ReqID:        qrResult.ReqID,
			Success:      qrResult.Success,
			ErrorMessage: qrResult.Message,
			Result:       nil, // null for failed requests
		}
		
		// Handle error response based on authentication method
		if isAPIKeyAuth {
			// For API key authentication: return only original_response
			utils.SendJSONResponse(w, http.StatusBadRequest, originalResponse)
		} else {
			// For Bearer token (frontend): return full error with user-friendly message
			apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, qrResult.Message, originalResponse)
			utils.SendErrorResponse(w, apiError)
		}
		return
	}

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
		Amount: 1,
	}
	
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Credits deduction failed - this is a serious issue since QR extraction was successful
		// In production, you might want to implement a compensation mechanism or retry logic
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"QR extraction completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Send different responses based on authentication method
	if isAPIKeyAuth {
		// For API key authentication: return only qrResult
		utils.SendJSONResponse(w, http.StatusOK, qrResult)
	} else {
		// For Bearer token (frontend): return full response with credits info
		response := &models.QRExtractionResponse{
			Message:          "QR extraction completed successfully",
			UserID:           user.UserID,
			RemainingCredits: updatedBalance.Credits,
			QRResult:         qrResult,
			ProcessedAt:      time.Now(),
		}
		utils.SendJSONResponse(w, http.StatusOK, response)
	}
}