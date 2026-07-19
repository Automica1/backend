package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"chi-mongo-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeAISaaSBetaServiceRepo struct {
	services []*models.BetaService
	err      error
}

func (f *fakeAISaaSBetaServiceRepo) Create(ctx context.Context, service *models.BetaService) error {
	return nil
}

func (f *fakeAISaaSBetaServiceRepo) GetByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return nil, nil
}

func (f *fakeAISaaSBetaServiceRepo) GetActiveByServiceName(ctx context.Context, serviceName string) (*models.BetaService, error) {
	return nil, nil
}

func (f *fakeAISaaSBetaServiceRepo) GetActiveByTag(ctx context.Context, tag string) (*models.BetaService, error) {
	return nil, nil
}

func (f *fakeAISaaSBetaServiceRepo) List(ctx context.Context, serviceName string, activeOnly bool) ([]*models.BetaService, error) {
	return f.services, f.err
}

func (f *fakeAISaaSBetaServiceRepo) Update(ctx context.Context, tag string, update bson.M) (*models.BetaService, error) {
	return nil, nil
}

type fakeAISaaSBetaKeyRepo struct {
	keys []*models.BetaKey
	err  error
}

func (f *fakeAISaaSBetaKeyRepo) Create(ctx context.Context, betaKey *models.BetaKey) error {
	return nil
}

func (f *fakeAISaaSBetaKeyRepo) GetActiveByHash(ctx context.Context, keyHash string) (*models.BetaKey, error) {
	return nil, nil
}

func (f *fakeAISaaSBetaKeyRepo) GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaKey, error) {
	return nil, nil
}

func (f *fakeAISaaSBetaKeyRepo) List(ctx context.Context, serviceName string) ([]*models.BetaKey, error) {
	return f.keys, f.err
}

func (f *fakeAISaaSBetaKeyRepo) Revoke(ctx context.Context, id primitive.ObjectID) error {
	return nil
}

func (f *fakeAISaaSBetaKeyRepo) UpdateLastUsed(ctx context.Context, keyHash string) error {
	return nil
}

type fakeAISaaSGPUPoolRepo struct {
	pools []*models.GPUPool
	err   error
}

func (f *fakeAISaaSGPUPoolRepo) GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return nil, nil
}

func (f *fakeAISaaSGPUPoolRepo) EnsurePool(ctx context.Context, serviceTag, serviceName string) (*models.GPUPool, error) {
	return nil, nil
}

func (f *fakeAISaaSGPUPoolRepo) Update(ctx context.Context, serviceTag string, update bson.M) (*models.GPUPool, error) {
	return nil, nil
}

func (f *fakeAISaaSGPUPoolRepo) List(ctx context.Context) ([]*models.GPUPool, error) {
	return f.pools, f.err
}

func (f *fakeAISaaSGPUPoolRepo) StartSession(ctx context.Context, serviceTag, userID string) (*models.GPUPool, bool, error) {
	return nil, false, nil
}

func (f *fakeAISaaSGPUPoolRepo) StopSession(ctx context.Context, serviceTag, userID string) (*models.GPUPool, error) {
	return nil, nil
}

func (f *fakeAISaaSGPUPoolRepo) StopSessionWithReason(ctx context.Context, serviceTag, userID, endReason string) (*models.GPUPool, error) {
	return nil, nil
}

func (f *fakeAISaaSGPUPoolRepo) UpdateActiveSession(ctx context.Context, serviceTag, userID string, update func(*models.GPUPoolSession)) (*models.GPUPool, error) {
	return nil, nil
}

func (f *fakeAISaaSGPUPoolRepo) AdminShutdown(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	return nil, nil
}

type fakeAISaaSProvisionConfigRepo struct {
	configs map[string]*models.GPUProvisionConfig
	errs    map[string]error
}

func (f *fakeAISaaSProvisionConfigRepo) GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUProvisionConfig, error) {
	if err, ok := f.errs[serviceTag]; ok {
		return nil, err
	}
	return f.configs[serviceTag], nil
}

func (f *fakeAISaaSProvisionConfigRepo) Upsert(ctx context.Context, cfg *models.GPUProvisionConfig) (*models.GPUProvisionConfig, error) {
	return nil, nil
}

type fakeAISaaSJobRepo struct {
	jobsByTag map[string][]*models.Job
	errs      map[string]error
}

func (f *fakeAISaaSJobRepo) Insert(ctx context.Context, job *models.Job) error { return nil }

func (f *fakeAISaaSJobRepo) GetByIdempotencyKey(ctx context.Context, key string) (*models.Job, error) {
	return nil, nil
}

func (f *fakeAISaaSJobRepo) HasActiveByIdempotencyKey(ctx context.Context, key string) (bool, error) {
	return false, nil
}

func (f *fakeAISaaSJobRepo) HasRunningGPUPoolDestroy(ctx context.Context, serviceTag string) (bool, error) {
	return false, nil
}

func (f *fakeAISaaSJobRepo) ReactivateByIdempotencyKey(ctx context.Context, key string, jobType string, payload map[string]any, runAfter time.Time, maxAttempts int) (*models.Job, error) {
	return nil, nil
}

func (f *fakeAISaaSJobRepo) ClaimNext(ctx context.Context, workerID string, now time.Time) (*models.Job, error) {
	return nil, nil
}

func (f *fakeAISaaSJobRepo) MarkCompleted(ctx context.Context, id primitive.ObjectID) error {
	return nil
}

func (f *fakeAISaaSJobRepo) MarkFailed(ctx context.Context, id primitive.ObjectID, errMsg string, retry bool, runAfter time.Time) error {
	return nil
}

func (f *fakeAISaaSJobRepo) MarkDead(ctx context.Context, id primitive.ObjectID, errMsg string) error {
	return nil
}

func (f *fakeAISaaSJobRepo) ReleaseStaleLocks(ctx context.Context, workerID string, staleBefore time.Time) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) ReleaseAllRunningLocks(ctx context.Context, reason string) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) CancelPendingByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) ReschedulePendingByIdempotencyKey(ctx context.Context, key string, runAfter time.Time) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) CancelRunningByIdempotencyKey(ctx context.Context, key string) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) MarkCancelled(ctx context.Context, id primitive.ObjectID) error {
	return nil
}

func (f *fakeAISaaSJobRepo) MarkDeadByIdempotencyKey(ctx context.Context, key, reason string) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) HasActiveMeterTick(ctx context.Context, serviceTag, userID string) (bool, error) {
	return false, nil
}

func (f *fakeAISaaSJobRepo) CancelPendingMeterTicks(ctx context.Context, serviceTag, userID string) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSJobRepo) GetByID(ctx context.Context, id primitive.ObjectID) (*models.Job, error) {
	return nil, nil
}

func (f *fakeAISaaSJobRepo) ListGPUByServiceTag(ctx context.Context, serviceTag string, limit int) ([]*models.Job, error) {
	if err, ok := f.errs[serviceTag]; ok {
		return nil, err
	}
	return f.jobsByTag[serviceTag], nil
}

type fakeAISaaSUsageService struct {
	stats []models.UsageStats
	err   error
}

func (f *fakeAISaaSUsageService) TrackUsage(ctx context.Context, req *models.UsageTrackingRequest) error {
	return nil
}

func (f *fakeAISaaSUsageService) GetGlobalStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error) {
	return f.stats, f.err
}

func (f *fakeAISaaSUsageService) GetUserStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UserUsageStats, error) {
	return nil, nil
}

func (f *fakeAISaaSUsageService) GetServiceUserStats(ctx context.Context, serviceName string, startDate, endDate *time.Time) ([]models.ServiceUserStats, error) {
	return nil, nil
}

func (f *fakeAISaaSUsageService) GetUserUsageHistory(ctx context.Context, userID string, startDate, endDate *time.Time, limit, skip int) ([]models.ServiceUsage, error) {
	return nil, nil
}

func (f *fakeAISaaSUsageService) GetServiceUsageHistory(ctx context.Context, serviceName string, startDate, endDate *time.Time, limit, skip int) ([]models.ServiceUsage, error) {
	return nil, nil
}

func (f *fakeAISaaSUsageService) GetAllUsageHistory(ctx context.Context, startDate, endDate *time.Time, limit, skip int) ([]models.ServiceUsage, error) {
	return nil, nil
}

func (f *fakeAISaaSUsageService) CountUsageHistory(ctx context.Context, startDate, endDate *time.Time) (int64, error) {
	return 0, nil
}

func (f *fakeAISaaSUsageService) CountServiceUsageHistory(ctx context.Context, serviceName string, startDate, endDate *time.Time) (int64, error) {
	return 0, nil
}

type fakeAISaaSFeedbackRepo struct {
	sessions []*models.BetaFeedbackSession
	err      error
}

func (f *fakeAISaaSFeedbackRepo) Create(ctx context.Context, session *models.BetaFeedbackSession) error {
	return nil
}

func (f *fakeAISaaSFeedbackRepo) GetByID(ctx context.Context, id primitive.ObjectID) (*models.BetaFeedbackSession, error) {
	return nil, nil
}

func (f *fakeAISaaSFeedbackRepo) GetPendingByUserAndService(ctx context.Context, userID, serviceName string) (*models.BetaFeedbackSession, error) {
	return nil, nil
}

func (f *fakeAISaaSFeedbackRepo) SupersedePendingByUserAndService(ctx context.Context, userID, serviceName string) error {
	return nil
}

func (f *fakeAISaaSFeedbackRepo) SubmitFeedback(ctx context.Context, id primitive.ObjectID, expected *models.BetaFeedbackExpectedResult, refundedCredits int) error {
	return nil
}

func (f *fakeAISaaSFeedbackRepo) SumRefundedCreditsSince(ctx context.Context, userID string, since time.Time) (int, error) {
	return 0, nil
}

func (f *fakeAISaaSFeedbackRepo) GetRefundUsageSince(ctx context.Context, userID string, since time.Time) (models.BetaFeedbackRefundUsage, error) {
	return models.BetaFeedbackRefundUsage{}, nil
}

func (f *fakeAISaaSFeedbackRepo) List(ctx context.Context, serviceName string, limit int) ([]*models.BetaFeedbackSession, error) {
	return f.sessions, f.err
}

type aiSaaSTestFixture struct {
	betaServices *fakeAISaaSBetaServiceRepo
	betaKeys     *fakeAISaaSBetaKeyRepo
	gpuPools     *fakeAISaaSGPUPoolRepo
	configs      *fakeAISaaSProvisionConfigRepo
	jobs         *fakeAISaaSJobRepo
	usage        *fakeAISaaSUsageService
	feedback     *fakeAISaaSFeedbackRepo
}

func newAISaaSTestFixture() *aiSaaSTestFixture {
	return &aiSaaSTestFixture{
		betaServices: &fakeAISaaSBetaServiceRepo{},
		betaKeys:     &fakeAISaaSBetaKeyRepo{},
		gpuPools:     &fakeAISaaSGPUPoolRepo{},
		configs:      &fakeAISaaSProvisionConfigRepo{},
		jobs:         &fakeAISaaSJobRepo{},
		usage:        &fakeAISaaSUsageService{},
		feedback:     &fakeAISaaSFeedbackRepo{},
	}
}

func (f *aiSaaSTestFixture) service() AISaaSService {
	return NewAISaaSService(
		f.betaServices,
		f.betaKeys,
		f.gpuPools,
		f.configs,
		f.jobs,
		f.usage,
		f.feedback,
	)
}

func findAISaaSServiceRow(t *testing.T, resp *models.AISaaSListResponse, apiID string) models.AISaaSServiceRecord {
	t.Helper()
	for _, row := range resp.Services {
		if row.APIID == apiID {
			return row
		}
	}
	t.Fatalf("expected apiId %q in response services", apiID)
	return models.AISaaSServiceRecord{}
}

func findAISaaSWarnings(warnings []models.AISaaSWarning, code string) []models.AISaaSWarning {
	var out []models.AISaaSWarning
	for _, warning := range warnings {
		if warning.Code == code {
			out = append(out, warning)
		}
	}
	return out
}

func TestAISaaSListServicesReportsSchemaVersionAndAPIIDFirstRows(t *testing.T) {
	now := time.Now().UTC()
	fixture := newAISaaSTestFixture()
	fixture.betaServices.services = []*models.BetaService{
		{
			Tag:         "ocr-gpu",
			ServiceName: "ocr",
			Label:       "OCR",
			APIURL:      "https://api.automica.ai/ocr",
			IsActive:    true,
		},
	}
	fixture.betaKeys.keys = []*models.BetaKey{
		{
			ServiceName:    "signature-verification",
			BetaServiceTag: "vlm-gpu",
			IsActive:       true,
		},
	}
	fixture.gpuPools.pools = []*models.GPUPool{
		{
			ServiceTag:  "vlm-gpu",
			ServiceName: "signature-verification",
			State:       models.GPUPoolStateReady,
			Sessions: []models.GPUPoolSession{
				{UserID: "user-1", StartedAt: now},
			},
			UpdatedAt: now,
		},
	}
	fixture.usage.stats = []models.UsageStats{
		{ServiceName: "ocr", TotalCalls: 12, SuccessCalls: 11, FailedCalls: 1, TotalCredits: 24},
	}

	resp, err := fixture.service().ListServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected ListServices to succeed, got %v", err)
	}

	if resp.SchemaVersion != models.AISaaSSchemaVersion {
		t.Fatalf("expected schema version %q, got %q", models.AISaaSSchemaVersion, resp.SchemaVersion)
	}

	for _, row := range resp.Services {
		if strings.HasPrefix(row.APIID, "unmapped-") {
			t.Fatalf("expected all rows to join canonical apiIds, found %q", row.APIID)
		}
	}

	ocr := findAISaaSServiceRow(t, resp, "ocr")
	if !ocr.Access.BetaSupported {
		t.Fatal("expected ocr beta service to mark the ocr apiId row as beta supported")
	}
	if ocr.Lifecycle != "active" {
		t.Fatalf("expected ocr lifecycle active, got %q", ocr.Lifecycle)
	}
	if ocr.Public.Endpoint != "https://api.automica.ai/ocr" {
		t.Fatalf("expected ocr endpoint from beta_services, got %q", ocr.Public.Endpoint)
	}
	if ocr.Public.DocsPath != "/api-docs" {
		t.Fatalf("expected deployed docs path /api-docs, got %q", ocr.Public.DocsPath)
	}
	if ocr.Usage.TotalCalls != 12 || ocr.Usage.TotalCredits != 24 {
		t.Fatalf("expected ocr usage joined by usage name, got %+v", ocr.Usage)
	}

	signature := findAISaaSServiceRow(t, resp, "signature-verification")
	if signature.Access.ActiveBetaKeys != 1 {
		t.Fatalf("expected 1 active beta key on signature-verification, got %d", signature.Access.ActiveBetaKeys)
	}
	if len(signature.Runtime) != 1 {
		t.Fatalf("expected one runtime on signature-verification, got %d", len(signature.Runtime))
	}
	runtime := signature.Runtime[0]
	if runtime.RuntimeID != "gpu:vlm-gpu" {
		t.Fatalf("expected gpu pool runtime joined by vlm-gpu tag, got %q", runtime.RuntimeID)
	}
	if runtime.Readiness != "ready" || runtime.ActiveSessions != 1 {
		t.Fatalf("expected ready runtime with one active session, got readiness=%q sessions=%d", runtime.Readiness, runtime.ActiveSessions)
	}
	if signature.Readiness != "ready" {
		t.Fatalf("expected signature-verification readiness ready, got %q", signature.Readiness)
	}
}

func TestAISaaSListServicesCollapsesLegacyVLMAliasIntoCanonicalRuntime(t *testing.T) {
	fixture := newAISaaSTestFixture()
	preferTrue := true
	fixture.betaServices.services = []*models.BetaService{
		{
			Tag:         "vlm-gpu",
			ServiceName: "signature-verification",
			APIURL:      "https://api.automica.ai/v1/sign_verify/vlm-gpu",
			RegistrySettings: &models.BetaServiceRegistrySettings{
				Provider:           "ecr",
				ImageTag:           "dev2",
				PreferRegistryPull: &preferTrue,
			},
		},
		{
			Tag:         "vlm-e2e-gpu",
			ServiceName: "signature-verification",
			APIURL:      "https://api.automica.ai/v1/sign_verify/vlm-e2e-gpu",
			RegistrySettings: &models.BetaServiceRegistrySettings{
				Provider:           "ghcr",
				ImageTag:           "legacy",
				PreferRegistryPull: &preferTrue,
			},
		},
	}
	fixture.gpuPools.pools = []*models.GPUPool{
		{
			ServiceTag:  "vlm-gpu",
			ServiceName: "sign_verify_vlm_gpu",
			State:       models.GPUPoolStateIdle,
			Provider:    "aws",
		},
		{
			ServiceTag:  "vlm-e2e-gpu",
			ServiceName: "sign_verify_vlm_gpu",
			State:       models.GPUPoolStateIdle,
			Provider:    "e2e",
		},
	}
	fixture.configs.configs = map[string]*models.GPUProvisionConfig{
		"vlm-gpu": {
			ServiceTag:  "vlm-gpu",
			ServiceName: "sign_verify_vlm_gpu",
			Infrastructure: models.GPUProvisionInfrastructure{
				PrimaryProvider: models.GPUProviderAWS,
			},
		},
		"vlm-e2e-gpu": {
			ServiceTag:  "vlm-e2e-gpu",
			ServiceName: "sign_verify_vlm_gpu",
			Infrastructure: models.GPUProvisionInfrastructure{
				PrimaryProvider: models.GPUProviderE2E,
			},
		},
	}

	resp, err := fixture.service().ListServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	signature := findAISaaSServiceRow(t, resp, "signature-verification")
	if len(signature.Runtime) != 1 {
		t.Fatalf("expected one collapsed runtime, got %d", len(signature.Runtime))
	}
	rt := signature.Runtime[0]
	if rt.RuntimeID != "gpu:vlm-gpu" || rt.ServiceTag != "vlm-gpu" {
		t.Fatalf("expected canonical runtime, got id=%q tag=%q", rt.RuntimeID, rt.ServiceTag)
	}
	if rt.State != string(models.GPUPoolStateIdle) || rt.Provider != "aws" {
		t.Fatalf("expected canonical idle/aws facts (not legacy e2e), got state=%q provider=%q", rt.State, rt.Provider)
	}
	if len(rt.RouteAliases) != 1 || rt.RouteAliases[0] != "vlm-e2e-gpu" {
		t.Fatalf("expected route alias vlm-e2e-gpu, got %#v", rt.RouteAliases)
	}
	if rt.Registry == nil || rt.Registry.Provider != "ecr" {
		t.Fatalf("expected canonical ECR registry to win, got %#v", rt.Registry)
	}
	if rt.Provision == nil || rt.Provision.PrimaryProvider != string(models.GPUProviderAWS) {
		t.Fatalf("expected canonical AWS provision to win, got %#v", rt.Provision)
	}
}

func TestAISaaSListServicesJoinsHistoricalSignatureBetaUsageName(t *testing.T) {
	fixture := newAISaaSTestFixture()
	fixture.usage.stats = []models.UsageStats{
		{ServiceName: "signature-verification-beta", TotalCalls: 120, SuccessCalls: 110, FailedCalls: 10, TotalCredits: 240},
		{ServiceName: "signature-verification", TotalCalls: 5, SuccessCalls: 5, TotalCredits: 10},
	}

	resp, err := fixture.service().ListServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected ListServices to succeed, got %v", err)
	}

	for _, row := range resp.Services {
		if strings.HasPrefix(row.APIID, "unmapped-") {
			t.Fatalf("expected no unmapped rows for historical usage names, found %q", row.APIID)
		}
	}

	signature := findAISaaSServiceRow(t, resp, "signature-verification")
	if signature.Usage.TotalCalls != 125 {
		t.Fatalf("expected 125 total calls joined onto signature-verification, got %d", signature.Usage.TotalCalls)
	}
	if signature.Usage.SuccessCalls != 115 || signature.Usage.FailedCalls != 10 {
		t.Fatalf("expected success/failed calls joined onto canonical row, got %+v", signature.Usage)
	}
	if signature.Usage.TotalCredits != 250 {
		t.Fatalf("expected 250 total credits joined onto signature-verification, got %d", signature.Usage.TotalCredits)
	}
	if len(findAISaaSWarnings(resp.Warnings, "unmapped_source_record")) != 0 {
		t.Fatal("expected no unmapped_source_record warning for the historical usage name")
	}
}

func TestAISaaSListServicesFlagsConflictingAlias(t *testing.T) {
	fixture := newAISaaSTestFixture()
	fixture.betaKeys.keys = []*models.BetaKey{
		{
			ServiceName:    "ocr",
			BetaServiceTag: "vlm-gpu",
			IsActive:       true,
		},
	}

	resp, err := fixture.service().ListServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected ListServices to succeed, got %v", err)
	}

	conflicts := findAISaaSWarnings(resp.Warnings, "conflicting_alias")
	if len(conflicts) == 0 {
		t.Fatal("expected conflicting_alias warning for beta key spanning ocr and vlm-gpu")
	}
	if conflicts[0].Severity != "error" {
		t.Fatalf("expected conflicting_alias severity error, got %q", conflicts[0].Severity)
	}
	if conflicts[0].APIID != "ocr" {
		t.Fatalf("expected conflict pinned to first-resolved apiId ocr, got %q", conflicts[0].APIID)
	}

	ocr := findAISaaSServiceRow(t, resp, "ocr")
	if len(findAISaaSWarnings(ocr.Warnings, "conflicting_alias")) == 0 {
		t.Fatal("expected conflicting_alias warning attached to the ocr row")
	}
	if ocr.Access.TotalBetaKeys != 1 {
		t.Fatalf("expected the conflicted key to stay on the first-resolved row, got %d keys", ocr.Access.TotalBetaKeys)
	}
}

func newAISaaSTestResolver(defs []AISaaSAliasDefinition) *AISaaSIdentityResolver {
	resolver := &AISaaSIdentityResolver{
		byAPIID: make(map[string]AISaaSAliasDefinition, len(defs)),
		byAlias: make(map[string][]aiSaaSAliasMatch),
	}
	for _, def := range defs {
		def = normalizeAISaaSAliasDefinition(def)
		resolver.definitions = append(resolver.definitions, def)
		resolver.byAPIID[def.APIID] = def
		resolver.index(def, aliasKindCatalogSlug, def.CatalogSlugs)
		resolver.index(def, aliasKindBetaServiceName, def.BetaServiceNames)
		resolver.index(def, aliasKindBetaServiceTag, def.BetaServiceTags)
		resolver.index(def, aliasKindGPUServiceTag, def.GPUServiceTags)
	}
	return resolver
}

func TestAISaaSBuilderFlagsAmbiguousAlias(t *testing.T) {
	resolver := newAISaaSTestResolver([]AISaaSAliasDefinition{
		{APIID: "svc-a", Slug: "svc-a", DisplayName: "Service A", BetaServiceTags: []string{"shared-gpu"}},
		{APIID: "svc-b", Slug: "svc-b", DisplayName: "Service B", BetaServiceTags: []string{"shared-gpu"}},
	})
	builder := newAISaaSBuilder(resolver, nil, nil)
	builder.applyBetaKey(&models.BetaKey{BetaServiceTag: "shared-gpu", IsActive: true})
	resp := builder.finalize()

	ambiguous := findAISaaSWarnings(resp.Warnings, "ambiguous_alias")
	if len(ambiguous) == 0 {
		t.Fatal("expected ambiguous_alias warning when one alias maps to multiple apiIds")
	}
	if ambiguous[0].Severity != "error" {
		t.Fatalf("expected ambiguous_alias severity error, got %q", ambiguous[0].Severity)
	}

	var unmapped *models.AISaaSServiceRecord
	for i := range resp.Services {
		if strings.HasPrefix(resp.Services[i].APIID, "unmapped-") {
			unmapped = &resp.Services[i]
			break
		}
	}
	if unmapped == nil {
		t.Fatal("expected the ambiguous record to fall back to an unmapped row instead of guessing an apiId")
	}
	if len(findAISaaSWarnings(unmapped.Warnings, "unmapped_source_record")) == 0 {
		t.Fatal("expected unmapped_source_record warning on the fallback row")
	}
	for _, apiID := range []string{"svc-a", "svc-b"} {
		row := findAISaaSServiceRow(t, resp, apiID)
		if row.Access.TotalBetaKeys != 0 {
			t.Fatalf("expected ambiguous key not to be joined to %s, got %d keys", apiID, row.Access.TotalBetaKeys)
		}
	}
}

func TestAISaaSListServicesReportsPartialSourceFailures(t *testing.T) {
	fixture := newAISaaSTestFixture()
	fixture.configs.configs = map[string]*models.GPUProvisionConfig{
		"ocr-gpu": {ServiceTag: "ocr-gpu", ServiceName: "ocr"},
	}
	fixture.configs.errs = map[string]error{
		"vlm-gpu": errors.New("provision config lookup timed out"),
	}
	fixture.usage.err = errors.New("usage aggregation unavailable")

	resp, err := fixture.service().ListServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected ListServices to degrade gracefully, got %v", err)
	}

	statusByName := map[string]models.AISaaSSourceStatus{}
	for _, source := range resp.Sources {
		statusByName[source.Name] = source
	}
	if got := statusByName["gpu_provision_configs"]; got.Status != "partial" {
		t.Fatalf("expected gpu_provision_configs source status partial, got %+v", got)
	}
	if got := statusByName["usage"]; got.Status != "error" {
		t.Fatalf("expected usage source status error, got %+v", got)
	}

	if len(findAISaaSWarnings(resp.Warnings, "source_partial")) == 0 {
		t.Fatal("expected source_partial warning for failed provision config lookups")
	}
	if len(findAISaaSWarnings(resp.Warnings, "source_unavailable")) == 0 {
		t.Fatal("expected source_unavailable warning for usage failure")
	}
	if resp.Fleet.PartialSourceCount < 2 {
		t.Fatalf("expected at least 2 degraded sources counted, got %d", resp.Fleet.PartialSourceCount)
	}
}

func TestAISaaSListServicesSerializedResponseRedactsSecrets(t *testing.T) {
	now := time.Now().UTC()
	fixture := newAISaaSTestFixture()
	fixture.gpuPools.pools = []*models.GPUPool{
		{
			ServiceTag:  "vlm-gpu",
			ServiceName: "signature-verification",
			State:       models.GPUPoolStateFailed,
			LastError:   "provision failed: mongodb://gpu-admin:sup3rSecretPw@mongo-primary:27017/jobs?tls=true",
			UpdatedAt:   now,
		},
	}
	fixture.jobs.jobsByTag = map[string][]*models.Job{
		"vlm-gpu": {
			{
				ID:             primitive.NewObjectID(),
				Type:           "gpu.pool.provision",
				Status:         models.JobStatusFailed,
				Attempts:       3,
				MaxAttempts:    3,
				RunAfter:       now,
				CreatedAt:      now,
				LastError:      "deploy failed api_key=live-key-123 AKIAABCDEFGHIJKLMNOP",
				IdempotencyKey: "gpu.pool.provision:vlm-gpu",
			},
		},
	}
	fixture.usage.err = errors.New("usage aggregation failed: password=hunter2 at mongodb://reporter:reportpw@mongo-analytics:27017/usage")
	fixture.betaKeys.err = errors.New("beta keys unavailable: token=beta-token-999")

	resp, err := fixture.service().ListServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected ListServices to degrade gracefully, got %v", err)
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("expected response to serialize, got %v", err)
	}
	serialized := string(payload)

	for _, secret := range []string{
		"sup3rSecretPw",
		"reportpw",
		"live-key-123",
		"AKIAABCDEFGHIJKLMNOP",
		"hunter2",
		"beta-token-999",
	} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("expected secret %q to be redacted from serialized response", secret)
		}
	}

	// json.Marshal HTML-escapes angle brackets, so match the marker word only.
	if !strings.Contains(serialized, "redacted") {
		t.Fatal("expected redaction markers in serialized response")
	}
	// Redaction must stay targeted: diagnostic context around secrets survives.
	if !strings.Contains(serialized, "mongodb://gpu-admin:") || !strings.Contains(serialized, "mongo-primary:27017") {
		t.Fatal("expected non-secret connection details to remain for diagnostics")
	}
	if !strings.Contains(serialized, "gpu.pool.provision:vlm-gpu") {
		t.Fatal("expected benign idempotency key to survive sanitization")
	}
}
