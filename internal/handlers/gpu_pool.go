package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

type GPUPoolHandler struct {
	gpuPoolService         services.GPUPoolService
	provisionConfigService services.GPUProvisionConfigService
	userService            services.UserService
	jobRepo                repository.JobRepository
	jobSvc                 services.JobService
	cfg                    *config.Config
}

func NewGPUPoolHandler(
	gpuPoolService services.GPUPoolService,
	provisionConfigService services.GPUProvisionConfigService,
	userService services.UserService,
	jobRepo repository.JobRepository,
	jobSvc services.JobService,
	cfg *config.Config,
) *GPUPoolHandler {
	return &GPUPoolHandler{
		gpuPoolService:         gpuPoolService,
		provisionConfigService: provisionConfigService,
		userService:            userService,
		jobRepo:                jobRepo,
		jobSvc:                 jobSvc,
		cfg:                    cfg,
	}
}

func (h *GPUPoolHandler) resolveUserID(r *http.Request) (string, error) {
	if userID, ok := middleware.GetUserIDFromContext(r.Context()); ok && userID != "" {
		return userID, nil
	}
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		return "", apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "authentication required")
	}
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		return "", err
	}
	return user.UserID, nil
}

func (h *GPUPoolHandler) Start(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	var req struct {
		ServiceTag string `json:"serviceTag"`
	}
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	status, err := h.gpuPoolService.Start(r.Context(), userID, req.ServiceTag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, status)
}

func (h *GPUPoolHandler) Stop(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	var req struct {
		ServiceTag string `json:"serviceTag"`
	}
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	status, err := h.gpuPoolService.Stop(r.Context(), userID, req.ServiceTag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, status)
}

func (h *GPUPoolHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := h.resolveUserID(r)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	serviceTag := r.URL.Query().Get("serviceTag")
	status, err := h.gpuPoolService.GetStatus(r.Context(), userID, serviceTag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, status)
}

func (h *GPUPoolHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	pools, err := h.gpuPoolService.ListAdmin(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	entries := make([]models.GPUPoolAdminEntry, 0, len(pools))
	for _, pool := range pools {
		if pool == nil {
			continue
		}
		entries = append(entries, h.enrichAdminPool(r.Context(), pool))
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{
		"pools":   entries,
		"billing": h.billingInfo(),
	})
}

func (h *GPUPoolHandler) AdminShutdown(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServiceTag string `json:"serviceTag"`
		Immediate  *bool  `json:"immediate"`
	}
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	immediate := false
	if req.Immediate != nil {
		immediate = *req.Immediate
	}

	pool, err := h.gpuPoolService.AdminShutdown(r.Context(), req.ServiceTag, immediate)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, pool)
}

func (h *GPUPoolHandler) AdminCancelGrace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServiceTag string `json:"serviceTag"`
	}
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	pool, err := h.gpuPoolService.AdminCancelGrace(r.Context(), req.ServiceTag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, pool)
}

func (h *GPUPoolHandler) GetProvisionConfig(w http.ResponseWriter, r *http.Request) {
	serviceTag := r.URL.Query().Get("serviceTag")
	if serviceTag == "" {
		serviceTag = chi.URLParam(r, "serviceTag")
	}
	if serviceTag == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "serviceTag is required"))
		return
	}
	cfg, err := h.provisionConfigService.GetOrDefault(r.Context(), serviceTag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{
		"policy":        cfg,
		"policySummary": h.provisionConfigService.EffectiveSummary(cfg),
	})
}

func (h *GPUPoolHandler) PutProvisionConfig(w http.ResponseWriter, r *http.Request) {
	serviceTag := chi.URLParam(r, "serviceTag")
	var cfg models.GPUProvisionConfig
	if err := utils.DecodeJSONBody(r, &cfg); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	if serviceTag != "" {
		cfg.ServiceTag = serviceTag
	}
	if cfg.ServiceTag == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "serviceTag is required"))
		return
	}
	if email, ok := middleware.GetEmailFromContext(r.Context()); ok {
		cfg.UpdatedBy = email
	}
	saved, err := h.provisionConfigService.Upsert(r.Context(), &cfg)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{
		"policy":        saved,
		"policySummary": h.provisionConfigService.EffectiveSummary(saved),
	})
}
