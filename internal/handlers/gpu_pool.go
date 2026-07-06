package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type GPUPoolHandler struct {
	gpuPoolService services.GPUPoolService
	userService    services.UserService
}

func NewGPUPoolHandler(gpuPoolService services.GPUPoolService, userService services.UserService) *GPUPoolHandler {
	return &GPUPoolHandler{gpuPoolService: gpuPoolService, userService: userService}
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
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{"pools": pools})
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
