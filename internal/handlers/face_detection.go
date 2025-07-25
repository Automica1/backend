// internal/handlers/face_detection.go
package handlers

import (
	"context"
	"net/http"
	"time"
	"fmt"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type FaceDetectionHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
	faceAPIService services.FaceDetectionAPIService
	errorMapper    *apperrors.APIErrorMapper
}

func NewFaceDetectionHandler(creditsService services.CreditsService, userService services.UserService, faceAPIService services.FaceDetectionAPIService) *FaceDetectionHandler {
	return &FaceDetectionHandler{
		creditsService: creditsService,
		userService:    userService,
		faceAPIService: faceAPIService,
		errorMapper:    apperrors.NewAPIErrorMapper(),
	}
}

func (h *FaceDetectionHandler) ProcessFaceDetection(w http.ResponseWriter, r *http.Request) {
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

	// Parse request body
	var req models.FaceDetectionRequest
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
			"insufficient credits for face detection operation (minimum 2 credits required)",
		))
		return
	}

	// Process face detection via external API
	faceResult, err := h.faceAPIService.ProcessFaceDetection(ctx, &req)
	if err != nil {
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
		// Use the error mapper to convert technical error to user-friendly message
		// Create original response object for the error
		originalResponse := map[string]interface{}{
			"req_id":        faceResult.ReqID,
			"success":       faceResult.Success,
			"error_message": faceResult.Message,
			"data":          faceResult.Data,
		}
		
		// Use the error mapper with original response
		apiError := apperrors.NewAPIErrorWithOriginalResponse(
			h.errorMapper, 
			faceResult.Message, 
			originalResponse,
		)
		utils.SendErrorResponse(w, apiError)
		return
	}

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
		Amount: 2,
	}
	
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Credits deduction failed - this is a serious issue since face detection was successful
		// In production, you might want to implement a compensation mechanism or retry logic
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"Face detection completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Prepare successful response with face detection results
	response := &models.FaceDetectionResponse{
		Message:          "Face detection completed successfully",
		UserID:           user.UserID,
		RemainingCredits: updatedBalance.Credits,
		FaceResult:       faceResult,
		ProcessedAt:      time.Now(),
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}