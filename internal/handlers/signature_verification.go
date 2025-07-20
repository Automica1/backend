// internal/handlers/signature_verification.go
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

type SignatureVerificationHandler struct {
	creditsService      services.CreditsService
	userService         services.UserService
	signatureAPIService services.SignatureVerificationAPIService
	errorMapper         *apperrors.APIErrorMapper
}

func NewSignatureVerificationHandler(creditsService services.CreditsService, userService services.UserService, signatureAPIService services.SignatureVerificationAPIService) *SignatureVerificationHandler {
	return &SignatureVerificationHandler{
		creditsService:      creditsService,
		userService:         userService,
		signatureAPIService: signatureAPIService,
		errorMapper:         apperrors.NewAPIErrorMapper(),
	}
}

func (h *SignatureVerificationHandler) ProcessSignatureVerification(w http.ResponseWriter, r *http.Request) {
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
	var req models.SignatureVerificationRequest
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
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second) // Longer timeout for signature verification
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
			"insufficient credits for signature verification operation (minimum 2 credits required)",
		))
		return
	}

	// Process signature verification via external API
	verificationResult, err := h.signatureAPIService.ProcessSignatureVerification(ctx, &req)
	if err != nil {
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
		// Create original response structure to match the API response format
		originalResponse := map[string]interface{}{
			"req_id":        verificationResult.ReqID,
			"success":       verificationResult.Success,
			"error_message": verificationResult.Message,
			"data":          map[string]interface{}{}, // Empty data object for failed requests
		}
		
		// Use the error mapper to convert technical error to user-friendly message with original response
		apiError := apperrors.NewAPIErrorWithOriginalResponse(h.errorMapper, verificationResult.Message, originalResponse)
		utils.SendErrorResponse(w, apiError)
		return
	}

	// API success: true - deduct 2 credits from user
	deductReq := &models.DeductCreditsRequest{
		UserID: user.UserID,
	}
	
	updatedBalance, err := h.creditsService.DeductCredits(ctx, deductReq)
	if err != nil {
		// Credits deduction failed - this is a serious issue since signature verification was successful
		// In production, you might want to implement a compensation mechanism or retry logic
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"signature verification completed but failed to deduct credits: "+err.Error(),
		))
		return
	}

	// Prepare successful response with verification result
	response := &models.SignatureVerificationResponse{
		Message:            "Signature verification completed successfully",
		UserID:             user.UserID,
		RemainingCredits:   updatedBalance.Credits,
		VerificationResult: verificationResult,
		ProcessedAt:        time.Now(),
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}