// internal/handlers/user.go
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

type UserHandler struct {
	userService services.UserService
}

func NewUserHandler(userService services.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

func (h *UserHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterUserRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.userService.RegisterUser(r.Context(), &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusCreated, response)
}

// Admin-only methods
func (h *UserHandler) GetAllUsers(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}
	response, err := h.userService.GetAllUsers(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *UserHandler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}
	userID := chi.URLParam(r, "userId")
	if userID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"userId parameter is required",
		))
		return
	}
	response, err := h.userService.GetUserByID(r.Context(), userID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *UserHandler) GetUserStats(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}
	response, err := h.userService.GetUserStats(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *UserHandler) GetUserActivity(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}
	userID := chi.URLParam(r, "userId")
	if userID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"userId parameter is required",
		))
		return
	}
	response, err := h.userService.GetUserActivity(r.Context(), userID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *UserHandler) GetUserCredits(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}
	userID := chi.URLParam(r, "userId")
	if userID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"userId parameter is required",
		))
		return
	}
	response, err := h.userService.GetUserCredits(r.Context(), userID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}