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

type BetaKeyHandler struct {
	betaKeyService services.BetaKeyService
	adminService   services.AdminService
}

func NewBetaKeyHandler(betaKeyService services.BetaKeyService, adminService services.AdminService) *BetaKeyHandler {
	return &BetaKeyHandler{
		betaKeyService: betaKeyService,
		adminService:   adminService,
	}
}

func (h *BetaKeyHandler) GenerateBetaKey(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	var req models.GenerateBetaKeyRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.betaKeyService.GenerateKey(r.Context(), &req, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "generate_beta_key",
			TargetType: "beta_key",
			TargetID:   response.KeyPrefix,
			Outcome:    "success",
			Metadata: map[string]interface{}{
				"serviceName":       req.ServiceName,
				"betaServiceTag":    req.BetaServiceTag,
				"label":             req.Label,
				"assignedUserEmail": req.AssignedUserEmail,
			},
		})
	}

	utils.SendJSONResponse(w, http.StatusCreated, response)
}

func (h *BetaKeyHandler) ListBetaKeys(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	keys, err := h.betaKeyService.ListKeys(r.Context(), serviceName)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	sanitized := make([]models.BetaKey, 0, len(keys))
	for _, key := range keys {
		item := key.Sanitize()
		sanitized = append(sanitized, item)
	}

	utils.SendJSONResponse(w, http.StatusOK, models.BetaKeyListResponse{
		Message: "Beta keys retrieved successfully",
		Keys:    sanitized,
		Total:   len(sanitized),
	})
}

func (h *BetaKeyHandler) RevokeBetaKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "keyId")
	email, _ := middleware.GetEmailFromContext(r.Context())

	response, err := h.betaKeyService.RevokeKey(r.Context(), keyID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil && email != "" {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "revoke_beta_key",
			TargetType: "beta_key",
			TargetID:   keyID,
			Outcome:    "success",
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *BetaKeyHandler) ResolveBetaKey(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	var req models.ResolveBetaKeyRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, 400, err.Error()))
		return
	}

	response, err := h.betaKeyService.ResolveKey(r.Context(), req.ServiceName, req.BetaKey, email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *BetaKeyHandler) ListSupportedServices(w http.ResponseWriter, r *http.Request) {
	services := make([]string, 0, len(models.SupportedBetaServices))
	for serviceName, supported := range models.SupportedBetaServices {
		if supported {
			services = append(services, serviceName)
		}
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message":  "Supported beta services",
		"services": services,
	})
}
