// internal/handlers/face_verification.go
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

type FaceVerificationHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
	faceAPIService services.FaceVerificationAPIService
	errorMapper    *apperrors.APIErrorMapper
}

func NewFaceVerificationHandler(creditsService services.CreditsService, userService services.UserService, faceAPIService services.FaceVerificationAPIService) *FaceVerificationHandler {
	return &FaceVerificationHandler{
		creditsService: creditsService,
		userService:    userService,
		faceAPIService: faceAPIService,
		errorMapper:    apperrors.NewAPIErrorMapper(),
	}
}

func (h *FaceVerificationHandler) ProcessFaceVerification(w http.ResponseWriter, r *http.Request) {
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
	var req models.FaceVerificationRequest
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
			"insufficient credits for face verification operation (minimum 2 credits required)",
		))
		return
	}

	// Process face verification via external API
	faceResult, err := h.faceAPIService.ProcessFaceVerification(ctx, &req)
	if err != nil {
		fmt.Printf("Face Verification API Error: %+v\n", err) // Debug log
		
		// Check if it's an AppError with original response
		if appErr, ok := err.(*apperrors.AppError); ok {
			// The service already processed the error and included original response
			utils.SendErrorResponse(w, appErr)
			return
		}
		
		// Fallback for other types of errors
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"Face verification operation failed: "+err.Error(),
		))
		return
	}

	fmt.Printf("Face Verification API Result: %+v\n", faceResult) // Debug log

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
		Amount: 2,
	}
	
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Credits deduction failed - this is a serious issue since face verification was successful
		// In production, you might want to implement a compensation mechanism or retry logic
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"Face verification completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Send different responses based on authentication method
	if isAPIKeyAuth {
		// For API key authentication: return only faceResult
		utils.SendJSONResponse(w, http.StatusOK, faceResult)
	} else {
		// For Bearer token (frontend): return full response with credits info
		response := &models.FaceVerificationResponse{
			Message:          "Face verification completed successfully",
			UserID:           user.UserID,
			RemainingCredits: updatedBalance.Credits,
			FaceResult:       faceResult,
			ProcessedAt:      time.Now(),
		}
		utils.SendJSONResponse(w, http.StatusOK, response)
	}
}