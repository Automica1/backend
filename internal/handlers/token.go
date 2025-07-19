// internal/handlers/token.go
package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

type TokenHandler struct {
	tokenService   services.CreditTokenService
	creditsService services.CreditsService
	userService    services.UserService
}

func NewTokenHandler(tokenService services.CreditTokenService, creditsService services.CreditsService, userService services.UserService) *TokenHandler {
	return &TokenHandler{
		tokenService:   tokenService,
		creditsService: creditsService,
		userService:    userService,
	}
}

func (h *TokenHandler) GenerateToken(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to generate tokens",
		))
		return
	}

	// Get admin email from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	var req models.GenerateTokenRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.tokenService.GenerateToken(r.Context(), &req, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusCreated, response)
}

func (h *TokenHandler) RedeemToken(w http.ResponseWriter, r *http.Request) {
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

	var req models.RedeemTokenRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Get user by email to get userID
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		// If user not found, try to auto-create them
		if apperrors.IsErrorType(err, apperrors.ErrUserNotFound) {
			registerReq := &models.RegisterUserRequest{
				UserID: email,
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
			
			// After successful creation, fetch the user again
			user, err = h.userService.GetUserByEmail(r.Context(), email)
			if err != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrInternalServer,
					http.StatusInternalServerError,
					"failed to fetch newly created user: "+err.Error(),
				))
				return
			}
		} else {
			utils.SendErrorResponse(w, err)
			return
		}
	}

	response, err := h.tokenService.RedeemToken(r.Context(), &req, user.UserID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *TokenHandler) GetMyTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view tokens",
		))
		return
	}

	// Get admin email from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	tokens, err := h.tokenService.GetTokensByCreatedBy(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Tokens retrieved successfully",
		"tokens":  tokens,
	})
}

// GetAllTokens - Admin only: Get all tokens in the system
func (h *TokenHandler) GetAllTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view all tokens",
		))
		return
	}

	tokens, err := h.tokenService.GetAllTokens(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "All tokens retrieved successfully",
		"count":   len(tokens),
		"tokens":  tokens,
	})
}

// GetUsedTokens - Admin only: Get all used tokens
func (h *TokenHandler) GetUsedTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view used tokens",
		))
		return
	}

	tokens, err := h.tokenService.GetTokensByStatus(r.Context(), true) // true = used tokens
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Used tokens retrieved successfully",
		"count":   len(tokens),
		"tokens":  tokens,
	})
}

// GetUnusedTokens - Admin only: Get all unused tokens
func (h *TokenHandler) GetUnusedTokens(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to view unused tokens",
		))
		return
	}

	tokens, err := h.tokenService.GetTokensByStatus(r.Context(), false) // false = unused tokens
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Unused tokens retrieved successfully",
		"count":   len(tokens),
		"tokens":  tokens,
	})
}

func (h *TokenHandler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required to delete tokens",
		))
		return
	}

	// Get admin email from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get tokenId from URL path parameter
	tokenID := chi.URLParam(r, "tokenId")
	if tokenID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrBadRequest,
			http.StatusBadRequest,
			"token ID is required",
		))
		return
	}

	response, err := h.tokenService.DeleteToken(r.Context(), tokenID, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}