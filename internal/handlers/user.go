// internal/handlers/user.go
package handlers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

type UserHandler struct {
	userService  services.UserService
	adminService services.AdminService
}

func NewUserHandler(userService services.UserService, adminService services.AdminService) *UserHandler {
	return &UserHandler{
		userService:  userService,
		adminService: adminService,
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

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
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

	if err := h.userService.DeleteUser(r.Context(), userID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if actorEmail, ok := middleware.GetEmailFromContext(r.Context()); ok && h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: actorEmail,
			Action:     "delete_user",
			TargetType: "user",
			TargetID:   userID,
			Outcome:    "success",
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{
		"message": "User deleted successfully",
		"userId":  userID,
	})
}

func (h *UserHandler) SuspendUser(w http.ResponseWriter, r *http.Request) {
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

	if err := h.userService.SuspendUser(r.Context(), userID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if actorEmail, ok := middleware.GetEmailFromContext(r.Context()); ok && h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: actorEmail,
			Action:     "suspend_user",
			TargetType: "user",
			TargetID:   userID,
			Outcome:    "success",
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{
		"message": "User suspended successfully",
		"userId":  userID,
	})
}

func (h *UserHandler) ReactivateUser(w http.ResponseWriter, r *http.Request) {
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

	if err := h.userService.ReactivateUser(r.Context(), userID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if actorEmail, ok := middleware.GetEmailFromContext(r.Context()); ok && h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: actorEmail,
			Action:     "reactivate_user",
			TargetType: "user",
			TargetID:   userID,
			Outcome:    "success",
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{
		"message": "User reactivated successfully",
		"userId":  userID,
	})
}

func (h *UserHandler) ExportUsersCSV(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	query := models.AdminListQuery{
		Limit:  100000,
		Skip:   0,
		Search: r.URL.Query().Get("search"),
	}

	users, err := h.userService.ListUsers(r.Context(), query)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"User ID", "Email", "Active", "Credits", "Created At", "Updated At"})
	for _, user := range users.Users {
		_ = writer.Write([]string{
			user.UserID,
			user.Email,
			strconv.FormatBool(user.IsActive),
			strconv.Itoa(user.Credits),
			user.CreatedAt.Format(time.RFC3339),
			user.UpdatedAt.Format(time.RFC3339),
		})
	}
	writer.Flush()

	filename := fmt.Sprintf("users-%s.csv", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
