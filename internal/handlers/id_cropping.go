// internal/handlers/id_cropping.go
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

type IDCroppingHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
	idAPIService   services.IDCroppingAPIService
	errorMapper    *apperrors.APIErrorMapper
}

func NewIDCroppingHandler(creditsService services.CreditsService, userService services.UserService, idAPIService services.IDCroppingAPIService) *IDCroppingHandler {
	return &IDCroppingHandler{
		creditsService: creditsService,
		userService:    userService,
		idAPIService:   idAPIService,
		errorMapper:    apperrors.NewAPIErrorMapper(),
	}
}

func (h *IDCroppingHandler) ProcessIDCropping(w http.ResponseWriter, r *http.Request) {
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
	var req models.IDCroppingRequest
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
			"insufficient credits for ID cropping operation (minimum 2 credits required)",
		))
		return
	}

	// Process ID cropping via external API
	cropResult, err := h.idAPIService.ProcessIDCropping(ctx, &req)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"ID cropping operation failed: "+err.Error(),
		))
		return
	}

	fmt.Printf("ID Cropping API Result: %+v\n", cropResult) // Debug log

	// Check if the API returned success
	if cropResult == nil || !cropResult.Success {
		// Use the original response that was set in the service if available
		var originalResponse interface{}
		if cropResult != nil && cropResult.OriginalResponse != nil {
			originalResponse = cropResult.OriginalResponse
		} else {
			// Fallback: construct from available data
			originalResponse = map[string]interface{}{
				"req_id":  cropResult.ReqID,
				"success": cropResult.Success,
			}
			
			// Add error_message if available
			if cropResult.Message != "" {
				originalResponse.(map[string]interface{})["error_message"] = cropResult.Message
			}
			
			// Add result field if it exists (even for failed requests)
			if cropResult.Result != nil {
				originalResponse.(map[string]interface{})["result"] = *cropResult.Result
			} else {
				// Add empty result field to match backend response format
				originalResponse.(map[string]interface{})["result"] = ""
			}
		}
		
		// Use the error mapper to convert technical error to user-friendly message with original response
		apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, cropResult.Message, originalResponse)
		utils.SendErrorResponse(w, apiError)
		return
	}

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
		Amount: 1,
	}
	
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Credits deduction failed - this is a serious issue since ID cropping was successful
		// In production, you might want to implement a compensation mechanism or retry logic
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"ID cropping completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Send different responses based on authentication method
	if isAPIKeyAuth {
		// For API key authentication: return only cropResult
		utils.SendJSONResponse(w, http.StatusOK, cropResult)
	} else {
		// For Bearer token (frontend): return full response with credits info
		response := &models.IDCroppingResponse{
			Message:          "ID cropping completed successfully",
			UserID:           user.UserID,
			RemainingCredits: updatedBalance.Credits,
			CropResult:       cropResult,
			ProcessedAt:      time.Now(),
		}
		utils.SendJSONResponse(w, http.StatusOK, response)
	}
}