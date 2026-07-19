package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
)

// AIServicesHandler is the thin HTTP layer for /api/v1/admin/ai-services.
// All identity resolution, safety checks, and audit logging live in the
// AIServicesFacade; this layer only decodes requests and shapes responses.
// serviceTag/betaServiceTag from clients are never authoritative — the facade
// resolves the concrete legacy owner from the apiId alias catalog.
type AIServicesHandler struct {
	facade              services.AIServicesFacade
	betaFeedbackService services.BetaFeedbackService
	gpuPoolHandler      *GPUPoolHandler
}

func NewAIServicesHandler(
	facade services.AIServicesFacade,
	betaFeedbackService services.BetaFeedbackService,
	gpuPoolHandler *GPUPoolHandler,
) *AIServicesHandler {
	return &AIServicesHandler{
		facade:              facade,
		betaFeedbackService: betaFeedbackService,
		gpuPoolHandler:      gpuPoolHandler,
	}
}

func aiServicesActor(r *http.Request) string {
	email, _ := middleware.GetEmailFromContext(r.Context())
	return email
}

// decodeOptionalJSON decodes a JSON body but tolerates an empty body, since
// several runtime ops carry no required parameters.
func decodeOptionalJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	return apperrors.NewAppError(apperrors.ErrBadRequest, http.StatusBadRequest, "invalid JSON format")
}

// ListServices serves the same aggregate as GET /admin/ai-saas/services.
func (h *AIServicesHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	startDate, endDate, err := parseAISaaSDateRange(r)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, err.Error()))
		return
	}

	response, err := h.facade.ListServices(r.Context(), startDate, endDate)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to build AI services ledger: "+err.Error(),
		))
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, response)
}

// GetService returns the single aggregate row for one apiId.
func (h *AIServicesHandler) GetService(w http.ResponseWriter, r *http.Request) {
	startDate, endDate, err := parseAISaaSDateRange(r)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, err.Error()))
		return
	}

	service, err := h.facade.GetService(r.Context(), chi.URLParam(r, "apiId"), startDate, endDate)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "AI service retrieved successfully",
		"service": service,
	})
}

// --- policy / registry / provision -----------------------------------------

type aiServicesPolicyRequest struct {
	RuntimeProfile string                `json:"runtimeProfile,omitempty"`
	ServicePolicy  *models.ServicePolicy `json:"servicePolicy"`
}

func (h *AIServicesHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	var req aiServicesPolicyRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	service, err := h.facade.UpdatePolicy(r.Context(), aiServicesActor(r), chi.URLParam(r, "apiId"), req.RuntimeProfile, req.ServicePolicy)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Service policy updated successfully",
		"service": service,
	})
}

type aiServicesRegistryRequest struct {
	RuntimeProfile   string                              `json:"runtimeProfile,omitempty"`
	RegistrySettings *models.BetaServiceRegistrySettings `json:"registrySettings"`
}

func (h *AIServicesHandler) UpdateRegistry(w http.ResponseWriter, r *http.Request) {
	var req aiServicesRegistryRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Registry settings persist no credentials (provider/auth mode/server/
	// namespace/image tag only); secrets stay in the pipeline env, so nothing
	// secret can echo back through the refreshed row.
	service, err := h.facade.UpdateRegistry(r.Context(), aiServicesActor(r), chi.URLParam(r, "apiId"), req.RuntimeProfile, req.RegistrySettings)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Registry settings updated successfully",
		"service": service,
	})
}

func (h *AIServicesHandler) ListRegistryImages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	catalog, err := h.facade.ListRegistryImages(
		r.Context(),
		chi.URLParam(r, "apiId"),
		q.Get("runtimeProfile"),
		q.Get("provider"),
		q.Get("image"),
	)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Registry image catalog",
		"catalog": catalog,
	})
}

type aiServicesProvisionRequest struct {
	RuntimeProfile    string                     `json:"runtimeProfile,omitempty"`
	ExpectedUpdatedAt *time.Time                 `json:"expectedUpdatedAt,omitempty"`
	Policy            *models.GPUProvisionConfig `json:"policy"`
}

func (h *AIServicesHandler) UpdateProvision(w http.ResponseWriter, r *http.Request) {
	var req aiServicesProvisionRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	saved, err := h.facade.UpdateProvision(r.Context(), aiServicesActor(r), chi.URLParam(r, "apiId"), req.RuntimeProfile, req.Policy, req.ExpectedUpdatedAt)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "GPU provision config updated successfully",
		"policy":  saved,
	})
}

// --- access / beta keys ------------------------------------------------------

type aiServicesIssueKeyRequest struct {
	RuntimeProfile    string `json:"runtimeProfile,omitempty"`
	Label             string `json:"label"`
	AssignedUserEmail string `json:"assignedUserEmail"`
	ExpiresInDays     *int   `json:"expiresInDays,omitempty"`
}

func (h *AIServicesHandler) IssueBetaKey(w http.ResponseWriter, r *http.Request) {
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrUnauthorized, http.StatusUnauthorized, "email not found in context"))
		return
	}

	var req aiServicesIssueKeyRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// ServiceName / BetaServiceTag are filled by the facade from the resolved
	// owner row, never from the client payload.
	response, err := h.facade.IssueBetaKey(r.Context(), email, chi.URLParam(r, "apiId"), req.RuntimeProfile, &models.GenerateBetaKeyRequest{
		Label:             req.Label,
		AssignedUserEmail: req.AssignedUserEmail,
		ExpiresInDays:     req.ExpiresInDays,
	})
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusCreated, response)
}

func (h *AIServicesHandler) RevokeBetaKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "keyId")
	if err := h.facade.RevokeBetaKey(r.Context(), aiServicesActor(r), chi.URLParam(r, "apiId"), keyID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Beta key revoked successfully",
		"keyId":   keyID,
	})
}

// --- runtime ops --------------------------------------------------------------

type aiServicesRuntimeRequest struct {
	RuntimeProfile string `json:"runtimeProfile,omitempty"`
	ExpectedState  string `json:"expectedState,omitempty"`
	ExtendMinutes  int    `json:"extendMinutes,omitempty"`
	Confirm        string `json:"confirm,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// runRuntimeAction is the shared wrapper for path-based runtime operations.
// The facade refetches pool state, enforces expectedState (409 on drift),
// enforces the destroy confirm token and active-session block, and audits.
// Idempotency keys are preserved because the facade delegates to the existing
// GPUPoolService job scheduling (gpu-pool:<tag>:provision / :destroy).
func (h *AIServicesHandler) runRuntimeAction(w http.ResponseWriter, r *http.Request, action string) {
	var req aiServicesRuntimeRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	if req.RuntimeProfile == "" {
		req.RuntimeProfile = r.URL.Query().Get("runtimeProfile")
	}

	apiID := chi.URLParam(r, "apiId")
	result, err := h.facade.RuntimeAction(r.Context(), aiServicesActor(r), apiID, services.AIServicesRuntimeActionRequest{
		Action:         action,
		RuntimeProfile: req.RuntimeProfile,
		Confirm:        req.Confirm,
		Immediate:      action == "destroy",
		ExtendMinutes:  req.ExtendMinutes,
		ExpectedState:  req.ExpectedState,
		Reason:         req.Reason,
	})
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Runtime operation completed successfully",
		"apiId":   apiID,
		"action":  action,
		"result":  result,
	})
}

// RuntimeAction is the generic body-driven variant: {"action": "warm-start", ...}.
// It shares all facade safety rules with the per-op routes.
func (h *AIServicesHandler) RuntimeAction(w http.ResponseWriter, r *http.Request) {
	var req services.AIServicesRuntimeActionRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	if req.RuntimeProfile == "" {
		req.RuntimeProfile = r.URL.Query().Get("runtimeProfile")
	}

	apiID := chi.URLParam(r, "apiId")
	result, err := h.facade.RuntimeAction(r.Context(), aiServicesActor(r), apiID, req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Runtime operation completed successfully",
		"apiId":   apiID,
		"action":  req.Action,
		"result":  result,
	})
}

func (h *AIServicesHandler) RuntimeWarmStart(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "warm-start")
}

func (h *AIServicesHandler) RuntimeRecover(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "recover")
}

func (h *AIServicesHandler) RuntimeRetryProvision(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "retry-provision")
}

func (h *AIServicesHandler) RuntimeAbortProvision(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "abort-provision")
}

// RuntimeGraceStop schedules a grace teardown (shutdown with grace).
func (h *AIServicesHandler) RuntimeGraceStop(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "grace-stop")
}

func (h *AIServicesHandler) RuntimeCancelGrace(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "cancel-grace")
}

func (h *AIServicesHandler) RuntimeExtendGrace(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "extend-grace")
}

// RuntimeDestroy is the immediate shutdown path: the facade requires
// confirm: "DESTROY" and refuses while sessions/refCount are active.
func (h *AIServicesHandler) RuntimeDestroy(w http.ResponseWriter, r *http.Request) {
	h.runRuntimeAction(w, r, "destroy")
}

// RuntimeDiagnostics resolves the owner serviceTag through the facade, audits
// the request, then delegates to the existing GPU diagnostics probe unchanged.
func (h *AIServicesHandler) RuntimeDiagnostics(w http.ResponseWriter, r *http.Request) {
	var req aiServicesRuntimeRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	if req.RuntimeProfile == "" {
		req.RuntimeProfile = r.URL.Query().Get("runtimeProfile")
	}

	apiID := chi.URLParam(r, "apiId")
	tag, err := h.facade.ResolveGPUServiceTag(apiID, req.RuntimeProfile)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	if h.gpuPoolHandler == nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrInternalServer, http.StatusInternalServerError, "diagnostics handler unavailable"))
		return
	}

	h.facade.RecordFacadeAudit(r.Context(), aiServicesActor(r), "ai_services.runtime.diagnostics", apiID, "requested", "", map[string]any{
		"gpuServiceTag": tag,
	})

	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("serviceTag", tag)
	h.gpuPoolHandler.RunDiagnostics(w, r)
}

// --- feedback -------------------------------------------------------------------

// ListFeedback wraps the existing beta feedback admin listing, scoped to the
// feedback service names resolved from the apiId alias catalog.
func (h *AIServicesHandler) ListFeedback(w http.ResponseWriter, r *http.Request) {
	def, err := h.facade.ResolveDefinition(chi.URLParam(r, "apiId"))
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 200 {
		limit = 200
	}

	merged := make([]models.BetaFeedbackSession, 0)
	for _, serviceName := range def.FeedbackServiceNames {
		sessions, listErr := h.betaFeedbackService.ListSessions(r.Context(), serviceName, limit)
		if listErr != nil {
			utils.SendErrorResponse(w, listErr)
			return
		}
		merged = append(merged, sessions...)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].CreatedAt.After(merged[j].CreatedAt) })
	if len(merged) > limit {
		merged = merged[:limit]
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message":      "Beta feedback sessions retrieved successfully",
		"apiId":        def.APIID,
		"serviceNames": def.FeedbackServiceNames,
		"sessions":     merged,
		"total":        len(merged),
	})
}

// ExportFeedback has no existing legacy export path to wrap; explicit 501
// instead of inventing a new data path.
func (h *AIServicesHandler) ExportFeedback(w http.ResponseWriter, r *http.Request) {
	utils.SendErrorResponse(w, apperrors.NewAppError(
		"NOT_IMPLEMENTED",
		http.StatusNotImplemented,
		"feedback export has no existing admin path to wrap yet; use GET /admin/ai-services/{apiId}/feedback",
	))
}
