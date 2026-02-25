// internal/handlers/plan.go
package handlers

import (
	"net/http"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

type PlanHandler struct {
	planService services.PlanService
}

func NewPlanHandler(planService services.PlanService) *PlanHandler {
	return &PlanHandler{
		planService: planService,
	}
}

// Public endpoints
func (h *PlanHandler) GetActivePlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.planService.GetActivePlans(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, plans)
}

// Admin endpoints
func (h *PlanHandler) GetAllPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.planService.GetAllPlans(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, plans)
}

func (h *PlanHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var req models.CreatePlanRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	plan, err := h.planService.CreatePlan(r.Context(), &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusCreated, plan)
}

func (h *PlanHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "planId")
	var req models.UpdatePlanRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	err := h.planService.UpdatePlan(r.Context(), planID, &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"message": "plan updated successfully"})
}

func (h *PlanHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "planId")
	err := h.planService.DeletePlan(r.Context(), planID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{"message": "plan deactivated successfully"})
}
