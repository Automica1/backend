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

type GuestPassHandler struct {
	guestPassService services.GuestPassService
	adminService     services.AdminService
}

func NewGuestPassHandler(guestPassService services.GuestPassService, adminService services.AdminService) *GuestPassHandler {
	return &GuestPassHandler{
		guestPassService: guestPassService,
		adminService:     adminService,
	}
}

func (h *GuestPassHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Guest-Pass")
	serviceSlug := r.URL.Query().Get("service")

	response, err := h.guestPassService.GetBalance(r.Context(), key, serviceSlug)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *GuestPassHandler) ValidatePass(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key         string `json:"key"`
		ServiceSlug string `json:"serviceSlug"`
	}
	_ = utils.DecodeJSONBody(r, &body)

	key := r.Header.Get("X-Guest-Pass")
	if key == "" {
		key = body.Key
	}

	serviceSlug := r.URL.Query().Get("service")
	if serviceSlug == "" {
		serviceSlug = body.ServiceSlug
	}

	_, err := h.guestPassService.ValidateKey(r.Context(), key)
	if err != nil {
		utils.SendJSONResponse(w, http.StatusOK, models.GuestPassValidateResponse{
			Message: "Invalid guest pass",
			Valid:   false,
		})
		return
	}

	balance, err := h.guestPassService.GetBalance(r.Context(), key, serviceSlug)
	if err != nil {
		utils.SendJSONResponse(w, http.StatusOK, models.GuestPassValidateResponse{
			Message: "Invalid guest pass",
			Valid:   false,
		})
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, models.GuestPassValidateResponse{
		Message:          "Guest pass is valid",
		Valid:            true,
		RemainingCredits: balance.RemainingCredits,
		AllowedServices:  balance.AllowedServices,
		ServiceAllowed:   balance.ServiceAllowed,
	})
}

func (h *GuestPassHandler) ListSupportedServices(w http.ResponseWriter, r *http.Request) {
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message":  "Supported services retrieved successfully",
		"services": h.guestPassService.ListSupportedServices(),
	})
}

func (h *GuestPassHandler) CreatePass(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	var req models.CreateGuestPassRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.guestPassService.CreatePass(r.Context(), &req, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "create_guest_pass",
			TargetType: "guest_pass",
			TargetID:   response.KeyPrefix,
			Outcome:    "success",
			Metadata: map[string]interface{}{
				"label":           req.Label,
				"credits":         req.Credits,
				"allowedServices": req.AllowedServices,
			},
		})
	}

	utils.SendJSONResponse(w, http.StatusCreated, response)
}

func (h *GuestPassHandler) ListPasses(w http.ResponseWriter, r *http.Request) {
	passes, err := h.guestPassService.ListPasses(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	sanitized := make([]models.GuestPass, 0, len(passes))
	for _, pass := range passes {
		item := pass.Sanitize()
		sanitized = append(sanitized, item)
	}

	utils.SendJSONResponse(w, http.StatusOK, models.GuestPassListResponse{
		Message: "Guest passes retrieved successfully",
		Passes:  sanitized,
		Total:   len(sanitized),
	})
}

func (h *GuestPassHandler) UpdatePass(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	passID := chi.URLParam(r, "passId")
	var req models.UpdateGuestPassRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	updated, err := h.guestPassService.UpdatePass(r.Context(), passID, &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "update_guest_pass",
			TargetType: "guest_pass",
			TargetID:   passID,
			Outcome:    "success",
		})
	}

	sanitized := updated.Sanitize()
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Guest pass updated successfully",
		"pass":    sanitized,
	})
}

func (h *GuestPassHandler) RevokePass(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	passID := chi.URLParam(r, "passId")
	response, err := h.guestPassService.RevokePass(r.Context(), passID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "revoke_guest_pass",
			TargetType: "guest_pass",
			TargetID:   passID,
			Outcome:    "success",
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}
