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

type PublicServicePolicyResponse struct {
	Message        string                `json:"message"`
	ServiceName    string                `json:"serviceName"`
	BetaServiceTag string                `json:"betaServiceTag,omitempty"`
	Label          string                `json:"label,omitempty"`
	ServicePolicy  *models.ServicePolicy `json:"servicePolicy,omitempty"`
}

type BetaServiceHandler struct {
	betaServiceService services.BetaServiceService
	adminService       services.AdminService
}

func NewBetaServiceHandler(betaServiceService services.BetaServiceService, adminService services.AdminService) *BetaServiceHandler {
	return &BetaServiceHandler{
		betaServiceService: betaServiceService,
		adminService:       adminService,
	}
}

func (h *BetaServiceHandler) ListBetaServices(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	activeOnly := r.URL.Query().Get("active") == "true"

	servicesList, err := h.betaServiceService.List(r.Context(), serviceName, activeOnly)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	items := make([]models.BetaService, 0, len(servicesList))
	for _, item := range servicesList {
		items = append(items, *item)
	}

	utils.SendJSONResponse(w, http.StatusOK, models.BetaServiceListResponse{
		Message:  "Beta services retrieved successfully",
		Services: items,
		Total:    len(items),
	})
}

func (h *BetaServiceHandler) GetBetaService(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	service, err := h.betaServiceService.GetByTag(r.Context(), tag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Beta service retrieved successfully",
		"service": service,
	})
}

func (h *BetaServiceHandler) CreateBetaService(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	var req models.CreateBetaServiceRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	service, err := h.betaServiceService.Create(r.Context(), &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "create_beta_service",
			TargetType: "beta_service",
			TargetID:   service.Tag,
			Outcome:    "success",
			Metadata: map[string]interface{}{
				"serviceName": service.ServiceName,
				"label":       service.Label,
			},
		})
	}

	utils.SendJSONResponse(w, http.StatusCreated, map[string]interface{}{
		"message": "Beta service created successfully",
		"service": service,
	})
}

func (h *BetaServiceHandler) UpdateBetaService(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	email, _ := middleware.GetEmailFromContext(r.Context())

	var req models.UpdateBetaServiceRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	service, err := h.betaServiceService.Update(r.Context(), tag, &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if h.adminService != nil && email != "" {
		_ = h.adminService.RecordAction(r.Context(), &models.AdminAuditLog{
			ActorEmail: email,
			Action:     "update_beta_service",
			TargetType: "beta_service",
			TargetID:   tag,
			Outcome:    "success",
		})
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Beta service updated successfully",
		"service": service,
	})
}

func (h *BetaServiceHandler) GetPublicServicePolicy(w http.ResponseWriter, r *http.Request) {
	serviceName := chi.URLParam(r, "serviceName")
	if serviceName == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "serviceName is required"))
		return
	}

	service, err := h.betaServiceService.GetActiveByServiceName(r.Context(), serviceName)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, PublicServicePolicyResponse{
		Message:        "Service policy resolved successfully",
		ServiceName:    service.ServiceName,
		BetaServiceTag: service.Tag,
		Label:          service.Label,
		ServicePolicy:  service.ServicePolicy,
	})
}
