package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"

	"github.com/go-chi/chi/v5"
)

// --- fakes -------------------------------------------------------------------

type fakeAIServicesAdmin struct {
	actions []models.AdminAuditLog
}

func (f *fakeAIServicesAdmin) RecordAction(ctx context.Context, log *models.AdminAuditLog) error {
	f.actions = append(f.actions, *log)
	return nil
}
func (f *fakeAIServicesAdmin) GetRecentAuditLogs(ctx context.Context, limit int) (*models.AdminAuditLogListResponse, error) {
	return nil, nil
}
func (f *fakeAIServicesAdmin) GetLogs(ctx context.Context, query models.AdminLogQuery) (*models.AdminLogListResponse, error) {
	return nil, nil
}
func (f *fakeAIServicesAdmin) Search(ctx context.Context, query string) (*models.AdminSearchResponse, error) {
	return nil, nil
}
func (f *fakeAIServicesAdmin) GetSummary(ctx context.Context) (*models.AdminSummaryResponse, error) {
	return nil, nil
}

type fakeAIServicesAISaaS struct{}

func (f *fakeAIServicesAISaaS) ListServices(ctx context.Context, startDate, endDate *time.Time) (*models.AISaaSListResponse, error) {
	return &models.AISaaSListResponse{
		Services: []models.AISaaSServiceRecord{{APIID: "ocr"}, {APIID: "signature-verification"}},
	}, nil
}

type fakeAIServicesBetaServices struct {
	updatedTag string
	updatedReq *models.UpdateBetaServiceRequest
}

func (f *fakeAIServicesBetaServices) Create(ctx context.Context, req *models.CreateBetaServiceRequest) (*models.BetaService, error) {
	return nil, nil
}
func (f *fakeAIServicesBetaServices) GetByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return &models.BetaService{Tag: tag, ServiceName: "ocr"}, nil
}
func (f *fakeAIServicesBetaServices) GetActiveByServiceName(ctx context.Context, serviceName string) (*models.BetaService, error) {
	return nil, nil
}
func (f *fakeAIServicesBetaServices) GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return nil, nil
}
func (f *fakeAIServicesBetaServices) List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error) {
	return nil, nil
}
func (f *fakeAIServicesBetaServices) Update(ctx context.Context, tag string, req *models.UpdateBetaServiceRequest) (*models.BetaService, error) {
	f.updatedTag = tag
	f.updatedReq = req
	return &models.BetaService{Tag: tag, ServiceName: "ocr", ServicePolicy: req.ServicePolicy}, nil
}

type fakeAIServicesGPUPools struct {
	shutdownTag       string
	shutdownImmediate bool
	shutdownCalls     int
}

func (f *fakeAIServicesGPUPools) Start(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) Stop(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) GetStatus(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) ListAdmin(ctx context.Context) ([]*models.GPUPool, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) AdminWarmStart(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return &models.GPUPool{ServiceTag: serviceTag, State: models.GPUPoolStateProvisioning}, nil
}
func (f *fakeAIServicesGPUPools) AdminShutdown(ctx context.Context, serviceTag string, immediate bool) (*models.GPUPool, error) {
	f.shutdownCalls++
	f.shutdownTag = serviceTag
	f.shutdownImmediate = immediate
	return &models.GPUPool{ServiceTag: serviceTag, State: models.GPUPoolStateDraining}, nil
}
func (f *fakeAIServicesGPUPools) AdminCancelGrace(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) AdminExtendGrace(ctx context.Context, serviceTag string, extend time.Duration) (*models.GPUPool, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) AdminAbortProvision(ctx context.Context, serviceTag string) error {
	return nil
}
func (f *fakeAIServicesGPUPools) AdminRetryProvision(ctx context.Context, serviceTag string) error {
	return nil
}
func (f *fakeAIServicesGPUPools) AdminRecover(ctx context.Context, serviceTag string) (*models.GPUPoolRecoveryReport, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) NodeClaimedByOtherPool(ctx context.Context, serviceTag, nodeID, publicIP string) (*models.GPUPool, error) {
	return nil, nil
}
func (f *fakeAIServicesGPUPools) ListInventory(ctx context.Context, serviceTag, providerOverride string) (*models.GPUPoolInventory, error) {
	return &models.GPUPoolInventory{ServiceTag: serviceTag, Source: "provider-list-nodes"}, nil
}
func (f *fakeAIServicesGPUPools) ReconcileIdleWarmPools(ctx context.Context) error  { return nil }
func (f *fakeAIServicesGPUPools) ReconcileOrphanPools(ctx context.Context) error    { return nil }
func (f *fakeAIServicesGPUPools) ReconcileScheduledState(ctx context.Context) error { return nil }
func (f *fakeAIServicesGPUPools) HandleMeterTick(ctx context.Context, serviceTag, userID string) error {
	return nil
}
func (f *fakeAIServicesGPUPools) HandleProvisionFailed(ctx context.Context, serviceTag string) error {
	return nil
}
func (f *fakeAIServicesGPUPools) OnProvisionSkippedNoSessions(ctx context.Context, serviceTag string) error {
	return nil
}
func (f *fakeAIServicesGPUPools) HandleProvisionNoSessions(ctx context.Context, serviceTag string, afterPipeline bool) (bool, error) {
	return false, nil
}

type fakeAIServicesPoolGetter struct {
	pool *models.GPUPool
}

func (f *fakeAIServicesPoolGetter) GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return f.pool, nil
}

// --- helpers -----------------------------------------------------------------

type aiServicesTestEnv struct {
	handler      *AIServicesHandler
	admin        *fakeAIServicesAdmin
	betaServices *fakeAIServicesBetaServices
	gpuPools     *fakeAIServicesGPUPools
}

func newAIServicesTestEnv(pool *models.GPUPool) *aiServicesTestEnv {
	admin := &fakeAIServicesAdmin{}
	betaServices := &fakeAIServicesBetaServices{}
	gpuPools := &fakeAIServicesGPUPools{}
	facade := services.NewAIServicesFacade(
		&fakeAIServicesAISaaS{},
		betaServices,
		nil, // beta keys not exercised in these tests
		gpuPools,
		nil, // provision config not exercised in these tests
		admin,
		&fakeAIServicesPoolGetter{pool: pool},
	)
	return &aiServicesTestEnv{
		handler:      NewAIServicesHandler(facade, nil, nil),
		admin:        admin,
		betaServices: betaServices,
		gpuPools:     gpuPools,
	}
}

func newAIServicesRequest(t *testing.T, method, target, apiID, body string) *http.Request {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("apiId", apiID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, "email", "admin@automica.ai") //nolint:staticcheck // matches auth middleware context key
	return req.WithContext(ctx)
}

func findAuditAction(actions []models.AdminAuditLog, action string) *models.AdminAuditLog {
	for i := range actions {
		if actions[i].Action == action {
			return &actions[i]
		}
	}
	return nil
}

// --- tests ---------------------------------------------------------------------

func TestAIServicesUpdatePolicyRejectsUnknownAPIID(t *testing.T) {
	env := newAIServicesTestEnv(nil)
	req := newAIServicesRequest(t, "PUT", "/api/v1/admin/ai-services/not-real/policy", "not-real",
		`{"servicePolicy":{"notes":"x"}}`)
	rec := httptest.NewRecorder()

	env.handler.UpdatePolicy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown apiId, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.betaServices.updatedTag != "" {
		t.Fatalf("expected no beta service update, got tag %q", env.betaServices.updatedTag)
	}
	if len(env.admin.actions) != 0 {
		t.Fatalf("expected no audit for rejected resolve, got %d", len(env.admin.actions))
	}
}

func TestAIServicesUpdatePolicyRejectsForeignRuntimeProfile(t *testing.T) {
	env := newAIServicesTestEnv(nil)
	// vlm-gpu belongs to signature-verification, not ocr — must not be
	// accepted as authority.
	req := newAIServicesRequest(t, "PUT", "/api/v1/admin/ai-services/ocr/policy", "ocr",
		`{"runtimeProfile":"vlm-gpu","servicePolicy":{"notes":"x"}}`)
	rec := httptest.NewRecorder()

	env.handler.UpdatePolicy(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for foreign runtimeProfile, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.betaServices.updatedTag != "" {
		t.Fatalf("expected no beta service update, got tag %q", env.betaServices.updatedTag)
	}
}

func TestAIServicesUpdatePolicyResolvesBetaTagAndAudits(t *testing.T) {
	env := newAIServicesTestEnv(nil)
	req := newAIServicesRequest(t, "PUT", "/api/v1/admin/ai-services/ocr/policy", "ocr",
		`{"servicePolicy":{"notes":"updated by facade test"}}`)
	rec := httptest.NewRecorder()

	env.handler.UpdatePolicy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.betaServices.updatedTag != "ocr-gpu" {
		t.Fatalf("expected resolved beta tag ocr-gpu, got %q", env.betaServices.updatedTag)
	}
	if env.betaServices.updatedReq == nil || env.betaServices.updatedReq.ServicePolicy == nil {
		t.Fatal("expected servicePolicy forwarded to beta service update")
	}
	audit := findAuditAction(env.admin.actions, "ai_services.policy.update")
	if audit == nil {
		t.Fatalf("expected policy update audit, got %+v", env.admin.actions)
	}
	if audit.Outcome != "success" || audit.ActorEmail != "admin@automica.ai" {
		t.Fatalf("unexpected audit entry: %+v", audit)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if payload["service"] == nil {
		t.Fatal("expected refreshed service row in response")
	}
}

func TestAIServicesDestroyRequiresConfirm(t *testing.T) {
	env := newAIServicesTestEnv(&models.GPUPool{ServiceTag: "ocr-gpu"})
	req := newAIServicesRequest(t, "POST", "/api/v1/admin/ai-services/ocr/runtime/destroy", "ocr", `{}`)
	rec := httptest.NewRecorder()

	env.handler.RuntimeDestroy(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without confirm token, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.gpuPools.shutdownCalls != 0 {
		t.Fatalf("expected no shutdown call, got %d", env.gpuPools.shutdownCalls)
	}
}

func TestAIServicesDestroyBlockedWhenSessionsActive(t *testing.T) {
	env := newAIServicesTestEnv(&models.GPUPool{
		ServiceTag: "ocr-gpu",
		RefCount:   1,
		Sessions:   []models.GPUPoolSession{{UserID: "u1"}},
	})
	req := newAIServicesRequest(t, "POST", "/api/v1/admin/ai-services/ocr/runtime/destroy", "ocr",
		`{"confirm":"DESTROY"}`)
	rec := httptest.NewRecorder()

	env.handler.RuntimeDestroy(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 with active sessions, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.gpuPools.shutdownCalls != 0 {
		t.Fatalf("expected no shutdown call, got %d", env.gpuPools.shutdownCalls)
	}
	audit := findAuditAction(env.admin.actions, "ai_services.runtime.destroy")
	if audit == nil || audit.Outcome != "error" {
		t.Fatalf("expected blocked destroy audit with error outcome, got %+v", env.admin.actions)
	}
}

func TestAIServicesDestroySucceedsAndAudits(t *testing.T) {
	env := newAIServicesTestEnv(&models.GPUPool{ServiceTag: "ocr-gpu", RefCount: 0})
	req := newAIServicesRequest(t, "POST", "/api/v1/admin/ai-services/ocr/runtime/destroy", "ocr",
		`{"confirm":"DESTROY"}`)
	rec := httptest.NewRecorder()

	env.handler.RuntimeDestroy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.gpuPools.shutdownCalls != 1 || env.gpuPools.shutdownTag != "ocr-gpu" || !env.gpuPools.shutdownImmediate {
		t.Fatalf("expected immediate shutdown of ocr-gpu, got calls=%d tag=%q immediate=%v",
			env.gpuPools.shutdownCalls, env.gpuPools.shutdownTag, env.gpuPools.shutdownImmediate)
	}
	audit := findAuditAction(env.admin.actions, "ai_services.runtime.destroy")
	if audit == nil || audit.Outcome != "success" {
		t.Fatalf("expected success destroy audit, got %+v", env.admin.actions)
	}
}

func TestAIServicesDestroyRejectsExpectedStateDrift(t *testing.T) {
	env := newAIServicesTestEnv(&models.GPUPool{ServiceTag: "ocr-gpu", State: models.GPUPoolStateReady})
	req := newAIServicesRequest(t, "POST", "/api/v1/admin/ai-services/ocr/runtime/destroy", "ocr",
		`{"confirm":"DESTROY","expectedState":"idle"}`)
	rec := httptest.NewRecorder()

	env.handler.RuntimeDestroy(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on expectedState drift, got %d: %s", rec.Code, rec.Body.String())
	}
	if env.gpuPools.shutdownCalls != 0 {
		t.Fatalf("expected no shutdown call on drift, got %d", env.gpuPools.shutdownCalls)
	}
}
