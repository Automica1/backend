// internal/handlers/credits.go
package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type CreditsHandler struct {
	creditsService services.CreditsService
	userService    services.UserService
}

func NewCreditsHandler(creditsService services.CreditsService, userService services.UserService) *CreditsHandler {
	return &CreditsHandler{
		creditsService: creditsService,
		userService:    userService,
	}
}

func (h *CreditsHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	// Get email from context (set by auth middleware)
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Try to get balance by email
	response, err := h.creditsService.GetBalanceByEmail(r.Context(), email)
	if err != nil {
		// If user not found, try to auto-create them
		if apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
			// Auto-create user with email as user_id for Kinde users
			registerReq := &models.RegisterUserRequest{
				UserID: email, // Use email as user_id for Kinde users
				Email:  email,
			}
			
			_, createErr := h.userService.RegisterUser(r.Context(), registerReq)
			if createErr != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to auto-create user: "+createErr.Error(),
				))
				return
			}

			// Now try to get balance again
			response, err = h.creditsService.GetBalanceByEmail(r.Context(), email)
			if err != nil {
				utils.SendErrorResponse(w, err)
				return
			}
		} else {
			utils.SendErrorResponse(w, err)
			return
		}
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *CreditsHandler) AddCredits(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to add credits",
		))
		return
	}

	var req models.AddCreditsRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.creditsService.AddCredits(r.Context(), &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *CreditsHandler) DeductCredits(w http.ResponseWriter, r *http.Request) {
	var req models.DeductCreditsRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.creditsService.DeductCredits(r.Context(), &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}