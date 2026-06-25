package handlers

import (
	"context"
	"net/http"
	"strconv"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

type BetaFeedbackHandler struct {
	betaFeedbackService services.BetaFeedbackService
	userService         services.UserService
	adminService        services.AdminService
}

func NewBetaFeedbackHandler(betaFeedbackService services.BetaFeedbackService, userService services.UserService, adminService services.AdminService) *BetaFeedbackHandler {
	return &BetaFeedbackHandler{
		betaFeedbackService: betaFeedbackService,
		userService:         userService,
		adminService:        adminService,
	}
}

func (h *BetaFeedbackHandler) resolveUserID(ctx context.Context, email string) (string, error) {
	if userID, ok := middleware.GetUserIDFromContext(ctx); ok && userID != "" {
		return userID, nil
	}
	user, err := h.userService.GetUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return user.UserID, nil
}

func (h *BetaFeedbackHandler) GetPendingFeedback(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	userID, err := h.resolveUserID(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	serviceName := r.URL.Query().Get("service")
	if serviceName == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "service query parameter is required"))
		return
	}

	session, err := h.betaFeedbackService.GetPendingSession(r.Context(), userID, serviceName)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, models.BetaFeedbackPendingResponse{
		Message: "Pending beta feedback retrieved successfully",
		Session: session,
	})
}

func (h *BetaFeedbackHandler) SubmitFeedback(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	userID, err := h.resolveUserID(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	var req models.SubmitBetaFeedbackRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.betaFeedbackService.SubmitFeedback(r.Context(), userID, email, sessionID, &req.ExpectedResult)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "beta_feedback_refund",
			TargetType: "beta_feedback_session",
			TargetID:   sessionID,
			Outcome:    "success",
			Metadata: map[string]interface{}{
				"creditsRefunded": response.CreditsRefunded,
			},
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *BetaFeedbackHandler) ListSessionsAdmin(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	sessions, err := h.betaFeedbackService.ListSessions(r.Context(), serviceName, limit)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, models.BetaFeedbackSessionListResponse{
		Message:  "Beta feedback sessions retrieved successfully",
		Sessions: sessions,
		Total:    len(sessions),
	})
}

func (h *BetaFeedbackHandler) GetSessionAdmin(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	session, err := h.betaFeedbackService.GetSessionByID(r.Context(), sessionID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, models.BetaFeedbackSessionDetailResponse{
		Message: "Beta feedback session retrieved successfully",
		Session: *session,
	})
}
