package services

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
)

const customerFacingAlignmentBlock = "customer-facing alignment gate: apiId mapping and regression tests required before writes"

var aiSaaSAWSAccessKeyPattern = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
var aiSaaSSecretKVPattern = regexp.MustCompile(`(?i)(password|secret|token|access[_-]?key|api[_-]?key)=([^ \t\n\r]+)`)

// Matches credentials embedded in connection URIs (for example
// mongodb://user:password@host) so raw driver errors never leak passwords.
var aiSaaSURLCredentialsPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/\s:@]+):([^@/\s]+)@`)

type AISaaSService interface {
	ListServices(ctx context.Context, startDate, endDate *time.Time) (*models.AISaaSListResponse, error)
}

type aiSaaSService struct {
	betaServiceRepo        repository.BetaServiceRepository
	betaKeyRepo            repository.BetaKeyRepository
	gpuPoolRepo            repository.GPUPoolRepository
	gpuProvisionConfigRepo repository.GPUProvisionConfigRepository
	jobRepo                repository.JobRepository
	usageService           UsageService
	betaFeedbackRepo       repository.BetaFeedbackRepository
	identity               *AISaaSIdentityResolver
}

func NewAISaaSService(
	betaServiceRepo repository.BetaServiceRepository,
	betaKeyRepo repository.BetaKeyRepository,
	gpuPoolRepo repository.GPUPoolRepository,
	gpuProvisionConfigRepo repository.GPUProvisionConfigRepository,
	jobRepo repository.JobRepository,
	usageService UsageService,
	betaFeedbackRepo repository.BetaFeedbackRepository,
) AISaaSService {
	return &aiSaaSService{
		betaServiceRepo:        betaServiceRepo,
		betaKeyRepo:            betaKeyRepo,
		gpuPoolRepo:            gpuPoolRepo,
		gpuProvisionConfigRepo: gpuProvisionConfigRepo,
		jobRepo:                jobRepo,
		usageService:           usageService,
		betaFeedbackRepo:       betaFeedbackRepo,
		identity:               NewAISaaSIdentityResolver(),
	}
}

func (s *aiSaaSService) ListServices(ctx context.Context, startDate, endDate *time.Time) (*models.AISaaSListResponse, error) {
	builder := newAISaaSBuilder(s.identity, startDate, endDate)

	betaServices, err := s.listBetaServices(ctx)
	builder.recordSource("beta_services", len(betaServices), err)
	if err == nil {
		for _, service := range betaServices {
			builder.applyBetaService(service)
		}
	}

	betaKeys, err := s.listBetaKeys(ctx)
	builder.recordSource("beta_keys", len(betaKeys), err)
	if err == nil {
		for _, key := range betaKeys {
			builder.applyBetaKey(key)
		}
	}

	gpuPools, err := s.listGPUPools(ctx)
	builder.recordSource("gpu_pools", len(gpuPools), err)
	if err == nil {
		for _, pool := range gpuPools {
			builder.applyGPUPool(pool)
		}
	}

	provisionTags := builder.knownRuntimeTags()
	provisionCount, provisionErrs := s.applyProvisionConfigs(ctx, builder, provisionTags)
	builder.recordMultiSource("gpu_provision_configs", provisionCount, provisionErrs)
	builder.addWarning(models.AISaaSWarning{
		Code:     "limited_gpu_config_discovery",
		Severity: "info",
		Source:   "gpu_provision_configs",
		Message:  "GPU provision configs are discovered by known aliases and runtime tags because the repository does not yet expose a safe list operation.",
	})

	jobCount, jobErrs := s.applyGPUJobs(ctx, builder, provisionTags)
	builder.recordMultiSource("jobs", jobCount, jobErrs)

	usageStats, err := s.listUsageStats(ctx, startDate, endDate)
	builder.recordSource("usage", len(usageStats), err)
	if err == nil {
		for _, stat := range usageStats {
			builder.applyUsage(stat)
		}
	}

	feedbackSessions, err := s.listFeedbackSessions(ctx)
	builder.recordSource("beta_feedback", len(feedbackSessions), err)
	if err == nil {
		for _, session := range feedbackSessions {
			builder.applyFeedback(session)
		}
	}

	return builder.finalize(), nil
}

func (s *aiSaaSService) listBetaServices(ctx context.Context) ([]*models.BetaService, error) {
	if s.betaServiceRepo == nil {
		return nil, fmt.Errorf("beta service repository is not configured")
	}
	return s.betaServiceRepo.List(ctx, "", false)
}

func (s *aiSaaSService) listBetaKeys(ctx context.Context) ([]*models.BetaKey, error) {
	if s.betaKeyRepo == nil {
		return nil, fmt.Errorf("beta key repository is not configured")
	}
	return s.betaKeyRepo.List(ctx, "")
}

func (s *aiSaaSService) listGPUPools(ctx context.Context) ([]*models.GPUPool, error) {
	if s.gpuPoolRepo == nil {
		return nil, fmt.Errorf("gpu pool repository is not configured")
	}
	return s.gpuPoolRepo.List(ctx)
}

func (s *aiSaaSService) listUsageStats(ctx context.Context, startDate, endDate *time.Time) ([]models.UsageStats, error) {
	if s.usageService == nil {
		return nil, fmt.Errorf("usage service is not configured")
	}
	return s.usageService.GetGlobalStats(ctx, startDate, endDate)
}

func (s *aiSaaSService) listFeedbackSessions(ctx context.Context) ([]*models.BetaFeedbackSession, error) {
	if s.betaFeedbackRepo == nil {
		return nil, fmt.Errorf("beta feedback repository is not configured")
	}
	return s.betaFeedbackRepo.List(ctx, "", 200)
}

func (s *aiSaaSService) applyProvisionConfigs(ctx context.Context, builder *aiSaaSBuilder, serviceTags []string) (int, []error) {
	if s.gpuProvisionConfigRepo == nil {
		return 0, []error{fmt.Errorf("gpu provision config repository is not configured")}
	}
	count := 0
	var errs []error
	for _, serviceTag := range serviceTags {
		cfg, err := s.gpuProvisionConfigRepo.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", serviceTag, err))
			continue
		}
		if cfg == nil {
			continue
		}
		count++
		builder.applyProvisionConfig(cfg)
	}
	return count, errs
}

func (s *aiSaaSService) applyGPUJobs(ctx context.Context, builder *aiSaaSBuilder, serviceTags []string) (int, []error) {
	if s.jobRepo == nil {
		return 0, []error{fmt.Errorf("job repository is not configured")}
	}
	count := 0
	var errs []error
	for _, serviceTag := range serviceTags {
		jobs, err := s.jobRepo.ListGPUByServiceTag(ctx, serviceTag, 5)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", serviceTag, err))
			continue
		}
		count += len(jobs)
		builder.applyGPUJobs(serviceTag, jobs)
	}
	return count, errs
}

type aiSaaSBuilder struct {
	identity     *AISaaSIdentityResolver
	generatedAt  time.Time
	startDate    *time.Time
	endDate      *time.Time
	rows         map[string]*models.AISaaSServiceRecord
	rowSources   map[string]map[string]int
	sourceStatus map[string]models.AISaaSSourceStatus
	warnings     []models.AISaaSWarning
}

func newAISaaSBuilder(identity *AISaaSIdentityResolver, startDate, endDate *time.Time) *aiSaaSBuilder {
	builder := &aiSaaSBuilder{
		identity:     identity,
		generatedAt:  time.Now().UTC(),
		startDate:    startDate,
		endDate:      endDate,
		rows:         map[string]*models.AISaaSServiceRecord{},
		rowSources:   map[string]map[string]int{},
		sourceStatus: map[string]models.AISaaSSourceStatus{},
	}
	for _, def := range identity.Definitions() {
		builder.ensureDefinitionRow(def)
	}
	return builder
}

func (b *aiSaaSBuilder) applyBetaService(service *models.BetaService) {
	if service == nil {
		return
	}
	row := b.rowForRecord(
		"beta_services",
		[]aliasCandidate{
			{kind: aliasKindBetaServiceName, value: service.ServiceName},
			{kind: aliasKindBetaServiceTag, value: service.Tag},
			{kind: aliasKindGPUServiceTag, value: service.Tag},
		},
	)
	b.recordRowSource(row.APIID, "beta_services", 1)
	row.Access.BetaSupported = true
	row.Access.BetaServiceTags = appendUnique(row.Access.BetaServiceTags, service.Tag)
	if service.IsActive {
		row.Lifecycle = bestLifecycle(row.Lifecycle, "active")
		row.Public.Status = bestPublicStatus(row.Public.Status, "active")
	} else {
		row.Lifecycle = bestLifecycle(row.Lifecycle, "inactive")
		if row.Public.Status == "" || row.Public.Status == "catalog" {
			row.Public.Status = "inactive"
		}
	}
	if service.APIURL != "" {
		row.Public.Endpoint = service.APIURL
		row.Public.Source = "beta_services"
	}
	if service.Label != "" && strings.HasPrefix(row.APIID, "unmapped-") {
		row.DisplayName = service.Label
	}
	if service.ServicePolicy != nil {
		row.Policy = servicePolicySummary(service.ServicePolicy, "beta_services")
	}
	if service.RegistrySettings != nil && !service.RegistrySettings.IsEmpty() {
		canonical := models.CanonicalGPUServiceTag(service.Tag)
		runtime := b.ensureRuntime(row, runtimeID(canonical))
		runtime.ServiceTag = canonical
		if service.ServiceName != "" {
			runtime.ServiceName = service.ServiceName
		}
		noteGPURouteAlias(runtime, service.Tag, canonical)
		// Canonical beta tag wins when both vlm-gpu and vlm-e2e-gpu carry registry.
		if service.Tag == canonical || runtime.Registry == nil {
			runtime.Registry = registrySummary(service.RegistrySettings, "beta_services")
		}
		if runtime.State == "" {
			runtime.State = "registry-configured"
		}
		if runtime.Readiness == "" {
			runtime.Readiness = "configured"
		}
	}
}

func (b *aiSaaSBuilder) applyBetaKey(key *models.BetaKey) {
	if key == nil {
		return
	}
	row := b.rowForRecord(
		"beta_keys",
		[]aliasCandidate{
			{kind: aliasKindBetaServiceName, value: key.ServiceName},
			{kind: aliasKindBetaServiceTag, value: key.BetaServiceTag},
			{kind: aliasKindGPUServiceTag, value: key.BetaServiceTag},
		},
	)
	b.recordRowSource(row.APIID, "beta_keys", 1)
	row.Access.BetaSupported = true
	row.Access.TotalBetaKeys++
	row.Access.BetaServiceTags = appendUnique(row.Access.BetaServiceTags, key.BetaServiceTag)
	switch {
	case key.IsValid():
		row.Access.ActiveBetaKeys++
	case key.RevokedAt != nil || !key.IsActive:
		row.Access.RevokedBetaKeys++
	case key.IsExpired():
		row.Access.ExpiredBetaKeys++
	default:
		row.Access.ExpiredBetaKeys++
	}
}

func (b *aiSaaSBuilder) applyGPUPool(pool *models.GPUPool) {
	if pool == nil {
		return
	}
	row := b.rowForRecord(
		"gpu_pools",
		[]aliasCandidate{
			{kind: aliasKindGPUServiceTag, value: pool.ServiceTag},
			{kind: aliasKindBetaServiceTag, value: pool.ServiceTag},
			{kind: aliasKindBetaServiceName, value: pool.ServiceName},
		},
	)
	b.recordRowSource(row.APIID, "gpu_pools", 1)
	canonical := models.CanonicalGPUServiceTag(pool.ServiceTag)
	runtime := b.ensureRuntime(row, runtimeID(canonical))
	noteGPURouteAlias(runtime, pool.ServiceTag, canonical)
	runtime.ServiceTag = canonical
	if pool.ServiceName != "" {
		runtime.ServiceName = pool.ServiceName
	}
	// Canonical pool owns live facts. Legacy tags (e.g. vlm-e2e-gpu) are route
	// aliases only — never let their idle/e2e leftovers paint Provider.
	if pool.ServiceTag != canonical {
		row.Readiness = bestReadiness(row.Readiness, runtime.Readiness)
		row.Lifecycle = bestLifecycle(row.Lifecycle, "runtime")
		return
	}
	runtime.State = string(pool.State)
	runtime.Readiness = readinessForGPUPool(pool.State)
	runtime.Provider = pool.Provider
	runtime.Region = pool.Region
	runtime.InstanceType = pool.InstanceType
	runtime.CapacityType = pool.CapacityType
	runtime.DeployVersion = pool.DeployVersion
	runtime.RefCount = pool.RefCount
	runtime.ActiveSessions = activeGPUSessions(pool.Sessions)
	runtime.NodeOwner = string(pool.NodeOwner)
	runtime.NodeID = pool.NodeID
	runtime.PublicIP = pool.PublicIP
	runtime.ReadyAt = pool.ReadyAt
	runtime.DestroyAt = pool.DestroyAt
	updatedAt := pool.UpdatedAt
	runtime.UpdatedAt = &updatedAt
	runtime.LastError = sanitizeAISaaSText(pool.LastError, 300)
	row.Readiness = bestReadiness(row.Readiness, runtime.Readiness)
	row.Lifecycle = bestLifecycle(row.Lifecycle, "runtime")
}

func (b *aiSaaSBuilder) applyProvisionConfig(cfg *models.GPUProvisionConfig) {
	if cfg == nil {
		return
	}
	cfg.Normalize()
	row := b.rowForRecord(
		"gpu_provision_configs",
		[]aliasCandidate{
			{kind: aliasKindGPUServiceTag, value: cfg.ServiceTag},
			{kind: aliasKindBetaServiceTag, value: cfg.ServiceTag},
			{kind: aliasKindBetaServiceName, value: cfg.ServiceName},
		},
	)
	b.recordRowSource(row.APIID, "gpu_provision_configs", 1)
	canonical := models.CanonicalGPUServiceTag(cfg.ServiceTag)
	runtime := b.ensureRuntime(row, runtimeID(canonical))
	noteGPURouteAlias(runtime, cfg.ServiceTag, canonical)
	runtime.ServiceTag = canonical
	if cfg.ServiceName != "" {
		runtime.ServiceName = cfg.ServiceName
	}
	// Canonical provision doc wins; skip divergent legacy copies.
	if cfg.ServiceTag == canonical || runtime.Provision == nil {
		runtime.Provision = provisionSummary(cfg)
	}
	if runtime.State == "" {
		runtime.State = "configured"
	}
	if runtime.Readiness == "" {
		runtime.Readiness = "configured"
	}
	if cfg.BlocksNewSessions() {
		b.addRowWarning(row.APIID, models.AISaaSWarning{
			Code:     "runtime_blocks_new_sessions",
			Severity: "warn",
			Source:   "gpu_provision_configs",
			APIID:    row.APIID,
			Message:  fmt.Sprintf("%s blocks new sessions through maintenance or blockNewSessions flags.", canonical),
		})
	}
}

func (b *aiSaaSBuilder) applyGPUJobs(serviceTag string, jobs []*models.Job) {
	if len(jobs) == 0 {
		return
	}
	row := b.rowForRecord(
		"jobs",
		[]aliasCandidate{
			{kind: aliasKindGPUServiceTag, value: serviceTag},
			{kind: aliasKindBetaServiceTag, value: serviceTag},
		},
	)
	b.recordRowSource(row.APIID, "jobs", len(jobs))
	canonical := models.CanonicalGPUServiceTag(serviceTag)
	runtime := b.ensureRuntime(row, runtimeID(canonical))
	noteGPURouteAlias(runtime, serviceTag, canonical)
	runtime.ServiceTag = canonical
	for _, job := range jobs {
		if job == nil {
			continue
		}
		runtime.Jobs = append(runtime.Jobs, jobSummary(job))
		if job.Status == models.JobStatusPending || job.Status == models.JobStatusRunning {
			row.Readiness = bestReadiness(row.Readiness, "provisioning")
			if runtime.Readiness == "" || runtime.Readiness == "configured" {
				runtime.Readiness = "provisioning"
			}
		}
	}
}

func noteGPURouteAlias(runtime *models.AISaaSRuntimeProfile, observedTag, canonical string) {
	if runtime == nil {
		return
	}
	observedTag = strings.TrimSpace(observedTag)
	canonical = strings.TrimSpace(canonical)
	if observedTag == "" || observedTag == canonical {
		return
	}
	runtime.RouteAliases = appendUnique(runtime.RouteAliases, observedTag)
}

func (b *aiSaaSBuilder) applyUsage(stat models.UsageStats) {
	row := b.rowForRecord("usage", []aliasCandidate{{kind: aliasKindUsageName, value: stat.ServiceName}})
	b.recordRowSource(row.APIID, "usage", 1)
	row.Usage.Source = "usage"
	row.Usage.TotalCalls += stat.TotalCalls
	row.Usage.SuccessCalls += stat.SuccessCalls
	row.Usage.FailedCalls += stat.FailedCalls
	row.Usage.TotalCredits += stat.TotalCredits
	if stat.TotalCalls > 0 {
		row.Lifecycle = bestLifecycle(row.Lifecycle, "active")
	}
}

func (b *aiSaaSBuilder) applyFeedback(session *models.BetaFeedbackSession) {
	if session == nil {
		return
	}
	row := b.rowForRecord(
		"beta_feedback",
		[]aliasCandidate{
			{kind: aliasKindFeedbackService, value: session.ServiceName},
			{kind: aliasKindBetaServiceName, value: session.ServiceName},
			{kind: aliasKindBetaServiceTag, value: session.BetaServiceTag},
		},
	)
	b.recordRowSource(row.APIID, "beta_feedback", 1)
	row.Feedback.Source = "beta_feedback"
	row.Feedback.RecentSessions++
	if session.Status == models.BetaFeedbackStatusPendingFeedback {
		row.Feedback.PendingSessions++
	}
	row.Feedback.RefundedCredits += session.CreditsRefunded
	if row.Feedback.LastCreatedAt == nil || session.CreatedAt.After(*row.Feedback.LastCreatedAt) {
		createdAt := session.CreatedAt
		row.Feedback.LastCreatedAt = &createdAt
	}
}

func (b *aiSaaSBuilder) finalize() *models.AISaaSListResponse {
	services := make([]models.AISaaSServiceRecord, 0, len(b.rows))
	fleet := models.AISaaSFleetSummary{}
	for _, row := range b.rows {
		b.finalizeRow(row)
		services = append(services, *row)
		fleet.TotalServices++
		if row.Public.Status == "active" || row.Public.Status == "catalog" {
			fleet.PublicServices++
		}
		if row.Access.BetaSupported {
			fleet.BetaServices++
		}
		if len(row.Runtime) > 0 {
			fleet.GPUBackedServices++
		}
		fleet.ActiveBetaKeys += row.Access.ActiveBetaKeys
		fleet.TotalCalls += row.Usage.TotalCalls
		fleet.TotalCredits += row.Usage.TotalCredits
		for _, runtime := range row.Runtime {
			fleet.ActiveSessions += runtime.ActiveSessions
			switch runtime.Readiness {
			case "ready":
				fleet.ReadyRuntimes++
			case "provisioning":
				fleet.Provisioning++
			case "failed":
				fleet.FailedRuntimes++
			}
		}
	}
	sort.Slice(services, func(i, j int) bool {
		if strings.HasPrefix(services[i].APIID, "unmapped-") != strings.HasPrefix(services[j].APIID, "unmapped-") {
			return !strings.HasPrefix(services[i].APIID, "unmapped-")
		}
		return services[i].DisplayName < services[j].DisplayName
	})

	sources := b.sortedSources()
	for _, source := range sources {
		if source.Status != "ok" {
			fleet.PartialSourceCount++
		}
	}

	return &models.AISaaSListResponse{
		SchemaVersion: models.AISaaSSchemaVersion,
		GeneratedAt:   b.generatedAt,
		DateRange: models.AISaaSDateRange{
			StartDate: b.startDate,
			EndDate:   b.endDate,
		},
		Sources:  sources,
		Warnings: b.warnings,
		Fleet:    fleet,
		Services: services,
	}
}

func (b *aiSaaSBuilder) finalizeRow(row *models.AISaaSServiceRecord) {
	row.Access.BetaServiceTags = uniqueSortedStrings(row.Access.BetaServiceTags)
	row.Runtime = sortedRuntime(row.Runtime)
	row.Links = serviceLinks(row)
	if row.Lifecycle == "" {
		row.Lifecycle = "catalog"
	}
	if row.Readiness == "" {
		row.Readiness = readinessFromRuntimes(row.Runtime)
	}
	if row.Public.Status == "" {
		row.Public.Status = "catalog"
	}
	if row.Public.DocsPath == "" {
		row.Public.DocsPath = "/api-docs"
	}
	if row.Public.TryAPIPath == "" {
		row.Public.TryAPIPath = "/services/" + row.Slug
	}
	// AI Services facade owns apiId-scoped policy/access writes; public contract stays read-only.
	row.Policy.Configurable = len(row.Aliases.BetaServiceTags) > 0
	if !row.Policy.Configurable {
		row.Policy.BlockedBy = "no beta service record for this API yet"
	} else {
		row.Policy.BlockedBy = ""
	}
	row.Access.Configurable = row.Access.BetaSupported || len(row.Aliases.BetaServiceTags) > 0
	if !row.Access.Configurable {
		row.Access.BlockedBy = "beta access is not enabled for this API"
	} else {
		row.Access.BlockedBy = ""
	}
	row.Public.Configurable = false
	row.Public.BlockedBy = "public contract is catalog-owned; edit docs/Try API from product surfaces"
	row.Sources = b.sortedRowSources(row.APIID)
	row.Warnings = sortedWarnings(row.Warnings)
	row.NextSafeAction = nextSafeAction(row)
}

func (b *aiSaaSBuilder) ensureDefinitionRow(def AISaaSAliasDefinition) *models.AISaaSServiceRecord {
	if row, ok := b.rows[def.APIID]; ok {
		return row
	}
	row := &models.AISaaSServiceRecord{
		APIID:       def.APIID,
		Slug:        def.Slug,
		DisplayName: def.DisplayName,
		Lifecycle:   "catalog",
		Readiness:   "public-only",
		Public: models.AISaaSPublicSummary{
			Status:     "catalog",
			DocsPath:   "/api-docs",
			TryAPIPath: "/services/" + def.Slug,
			Source:     "frontend_catalog_alias",
		},
		Aliases: b.identity.ToModelAliases(def),
		Access: models.AISaaSAccessSummary{
			Model: "standard",
		},
	}
	b.rows[row.APIID] = row
	return row
}

func (b *aiSaaSBuilder) ensureUnmappedRow(source string, candidates []aliasCandidate) *models.AISaaSServiceRecord {
	kind := "source"
	value := source
	for _, candidate := range candidates {
		if normalizeAISaaSAlias(candidate.value) != "" {
			kind = candidate.kind
			value = candidate.value
			break
		}
	}
	apiID := "unmapped-" + normalizeAISaaSAlias(kind+"-"+value)
	if apiID == "unmapped-" {
		apiID = "unmapped-source"
	}
	if row, ok := b.rows[apiID]; ok {
		return row
	}
	aliasValue := normalizeAISaaSAlias(value)
	row := &models.AISaaSServiceRecord{
		APIID:       apiID,
		Slug:        apiID,
		DisplayName: "Unmapped " + strings.TrimSpace(value),
		Lifecycle:   "unmapped",
		Readiness:   "warning",
		Public: models.AISaaSPublicSummary{
			Status: "unmapped",
			Source: source,
		},
		Access: models.AISaaSAccessSummary{
			Model: "unknown",
		},
	}
	switch kind {
	case aliasKindUsageName:
		row.Aliases.UsageNames = []string{aliasValue}
	case aliasKindBetaServiceName:
		row.Aliases.BetaServiceNames = []string{aliasValue}
	case aliasKindBetaServiceTag:
		row.Aliases.BetaServiceTags = []string{aliasValue}
	case aliasKindGPUServiceTag:
		row.Aliases.GPUServiceTags = []string{aliasValue}
	case aliasKindPipelineService:
		row.Aliases.PipelineServices = []string{aliasValue}
	case aliasKindFeedbackService:
		row.Aliases.FeedbackServiceNames = []string{aliasValue}
	default:
		row.Aliases.CatalogSlugs = []string{aliasValue}
	}
	b.rows[row.APIID] = row
	b.addRowWarning(row.APIID, models.AISaaSWarning{
		Code:     "unmapped_source_record",
		Severity: "warn",
		Source:   source,
		APIID:    row.APIID,
		Message:  fmt.Sprintf("%s record %q is not mapped to a canonical apiId alias.", source, value),
	})
	return row
}

type aliasCandidate struct {
	kind  string
	value string
}

func (b *aiSaaSBuilder) rowForRecord(source string, candidates []aliasCandidate) *models.AISaaSServiceRecord {
	apiID := ""
	for _, candidate := range candidates {
		if normalizeAISaaSAlias(candidate.value) == "" {
			continue
		}
		matches := b.identity.Matches(candidate.kind, candidate.value)
		if len(matches) == 0 {
			continue
		}
		if len(matches) > 1 {
			b.addWarning(models.AISaaSWarning{
				Code:     "ambiguous_alias",
				Severity: "error",
				Source:   source,
				Message:  fmt.Sprintf("%s value %q maps to multiple apiIds and was not joined by that alias.", candidate.kind, candidate.value),
			})
			continue
		}
		if apiID == "" {
			apiID = matches[0].APIID
			continue
		}
		if apiID != matches[0].APIID {
			b.addRowWarning(apiID, models.AISaaSWarning{
				Code:     "conflicting_alias",
				Severity: "error",
				Source:   source,
				APIID:    apiID,
				Message:  fmt.Sprintf("%s value %q maps to %s, but the record was already resolved to %s.", candidate.kind, candidate.value, matches[0].APIID, apiID),
			})
		}
	}
	if apiID != "" {
		if def, ok := b.identity.Definition(apiID); ok {
			return b.ensureDefinitionRow(def)
		}
	}
	return b.ensureUnmappedRow(source, candidates)
}

func (b *aiSaaSBuilder) ensureRuntime(row *models.AISaaSServiceRecord, id string) *models.AISaaSRuntimeProfile {
	if id == "" {
		id = "runtime"
	}
	for i := range row.Runtime {
		if row.Runtime[i].RuntimeID == id {
			return &row.Runtime[i]
		}
	}
	row.Runtime = append(row.Runtime, models.AISaaSRuntimeProfile{
		RuntimeID: id,
		State:     "configured",
		Readiness: "configured",
	})
	return &row.Runtime[len(row.Runtime)-1]
}

func (b *aiSaaSBuilder) knownRuntimeTags() []string {
	tags := map[string]bool{}
	for _, def := range b.identity.Definitions() {
		for _, tag := range def.GPUServiceTags {
			tags[models.CanonicalGPUServiceTag(tag)] = true
		}
		for _, tag := range def.BetaServiceTags {
			tags[models.CanonicalGPUServiceTag(tag)] = true
		}
	}
	for _, row := range b.rows {
		for _, tag := range row.Access.BetaServiceTags {
			tags[models.CanonicalGPUServiceTag(tag)] = true
		}
		for _, runtime := range row.Runtime {
			if runtime.ServiceTag != "" {
				tags[models.CanonicalGPUServiceTag(runtime.ServiceTag)] = true
			}
		}
	}
	out := make([]string, 0, len(tags))
	for tag := range tags {
		if tag == "" {
			continue
		}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func (b *aiSaaSBuilder) recordSource(name string, count int, err error) {
	status := models.AISaaSSourceStatus{Name: name, Count: count, Status: "ok"}
	if err != nil {
		status.Status = "error"
		status.Warning = sanitizeAISaaSText(err.Error(), 240)
		b.addWarning(models.AISaaSWarning{
			Code:     "source_unavailable",
			Severity: "warn",
			Source:   name,
			Message:  fmt.Sprintf("%s source failed: %s", name, sanitizeAISaaSText(err.Error(), 180)),
		})
	}
	b.sourceStatus[name] = status
}

func (b *aiSaaSBuilder) recordMultiSource(name string, count int, errs []error) {
	status := models.AISaaSSourceStatus{Name: name, Count: count, Status: "ok"}
	if len(errs) > 0 {
		status.Status = "partial"
		if count == 0 {
			status.Status = "error"
		}
		status.Warning = fmt.Sprintf("%d lookup(s) failed", len(errs))
		b.addWarning(models.AISaaSWarning{
			Code:     "source_partial",
			Severity: "warn",
			Source:   name,
			Message:  fmt.Sprintf("%s source had %d failed lookup(s). First error: %s", name, len(errs), sanitizeAISaaSText(errs[0].Error(), 160)),
		})
	}
	b.sourceStatus[name] = status
}

func (b *aiSaaSBuilder) recordRowSource(apiID, source string, count int) {
	if apiID == "" || source == "" || count <= 0 {
		return
	}
	if _, ok := b.rowSources[apiID]; !ok {
		b.rowSources[apiID] = map[string]int{}
	}
	b.rowSources[apiID][source] += count
}

func (b *aiSaaSBuilder) sortedSources() []models.AISaaSSourceStatus {
	names := make([]string, 0, len(b.sourceStatus))
	for name := range b.sourceStatus {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]models.AISaaSSourceStatus, 0, len(names))
	for _, name := range names {
		out = append(out, b.sourceStatus[name])
	}
	return out
}

func (b *aiSaaSBuilder) sortedRowSources(apiID string) []models.AISaaSSourceStatus {
	counts := b.rowSources[apiID]
	if len(counts) == 0 {
		return nil
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]models.AISaaSSourceStatus, 0, len(names))
	for _, name := range names {
		out = append(out, models.AISaaSSourceStatus{Name: name, Count: counts[name], Status: "ok"})
	}
	return out
}

func (b *aiSaaSBuilder) addWarning(warning models.AISaaSWarning) {
	if warning.Severity == "" {
		warning.Severity = "warn"
	}
	b.warnings = append(b.warnings, warning)
}

func (b *aiSaaSBuilder) addRowWarning(apiID string, warning models.AISaaSWarning) {
	if warning.Severity == "" {
		warning.Severity = "warn"
	}
	if warning.APIID == "" {
		warning.APIID = apiID
	}
	if row, ok := b.rows[apiID]; ok {
		row.Warnings = append(row.Warnings, warning)
	}
	b.addWarning(warning)
}

func servicePolicySummary(policy *models.ServicePolicy, source string) models.AISaaSPolicySummary {
	policy.Normalize()
	summary := models.AISaaSPolicySummary{
		Source:    source,
		HasPolicy: true,
		Summary:   policy.Summary(),
		Notes:     policy.Notes,
		// Configurable is finalized per-row in finalizeRow once aliases are known.
		Configurable: true,
	}
	if policy.Limits != nil {
		summary.MaxUploadSizeMB = policy.Limits.MaxUploadSizeMB
		summary.MaxPages = policy.Limits.MaxPages
		summary.MaxFiles = policy.Limits.MaxFiles
		summary.AllowedFormats = append([]string{}, policy.Limits.AllowedFormats...)
	}
	if policy.Pricing != nil {
		summary.PricingMode = policy.Pricing.Mode
		summary.CreditsPerHit = policy.Pricing.CreditsPerHit
		summary.CreditsPerPage = policy.Pricing.CreditsPerPage
		summary.StartupCredits = policy.Pricing.StartupCredits
		summary.CreditsPerMinute = policy.Pricing.CreditsPerMinute
	}
	return summary
}

func registrySummary(settings *models.BetaServiceRegistrySettings, source string) *models.AISaaSRegistrySummary {
	if settings == nil {
		return nil
	}
	copySettings := *settings
	copySettings.Normalize()
	return &models.AISaaSRegistrySummary{
		Source:             source,
		Provider:           copySettings.Provider,
		AuthMode:           copySettings.Auth,
		Server:             copySettings.Server,
		Namespace:          copySettings.Namespace,
		Region:             copySettings.Region,
		ImageTag:           copySettings.ImageTag,
		PreferRegistryPull: copySettings.PreferRegistryPull,
		LoginRequired:      copySettings.LoginRequired,
	}
}

func provisionSummary(cfg *models.GPUProvisionConfig) *models.AISaaSProvisionSummary {
	if cfg == nil {
		return nil
	}
	cfg.Normalize()
	infra := cfg.Infrastructure
	return &models.AISaaSProvisionSummary{
		Source:               "gpu_provision_configs",
		PrimaryProvider:      string(infra.PrimaryProvider),
		FallbackProvider:     string(infra.FallbackProvider),
		AWSRegion:            infra.AWS.Region,
		AWSInstanceType:      infra.AWS.InstanceType,
		AWSCapacityType:      firstNonEmptyString(infra.AWS.CapacityType, infra.AWS.CapacityFallback, infra.AWS.FallbackCapacity),
		E2ELocation:          infra.E2E.Location,
		E2EGPUCard:           infra.E2E.GPUCard,
		GCPRegion:            infra.GCP.Region,
		GCPMachineType:       infra.GCP.MachineType,
		MaintenanceMode:      cfg.Flags.MaintenanceMode,
		BlockNewSessions:     cfg.Flags.BlockNewSessions,
		MaintenanceMessage:   cfg.Flags.MaintenanceMessage,
		GracePeriodMin:       cfg.Lifecycle.GracePeriodMin,
		DeployHealthSec:      cfg.Timeouts.DeployHealthSec,
		ProvisionMaxAttempts: cfg.Retries.ProvisionMaxAttempts,
	}
}

func jobSummary(job *models.Job) models.AISaaSJobSummary {
	return models.AISaaSJobSummary{
		ID:             job.ID.Hex(),
		Type:           job.Type,
		Status:         string(job.Status),
		Attempts:       job.Attempts,
		MaxAttempts:    job.MaxAttempts,
		RunAfter:       job.RunAfter,
		CreatedAt:      job.CreatedAt,
		CompletedAt:    job.CompletedAt,
		LastError:      sanitizeAISaaSText(job.LastError, 240),
		IdempotencyKey: sanitizeAISaaSText(job.IdempotencyKey, 160),
	}
}

func activeGPUSessions(sessions []models.GPUPoolSession) int {
	count := 0
	for _, session := range sessions {
		if session.StoppedAt == nil {
			count++
		}
	}
	return count
}

func runtimeID(serviceTag string) string {
	tag := normalizeAISaaSAlias(serviceTag)
	if tag == "" {
		return "runtime"
	}
	return "gpu:" + tag
}

func readinessForGPUPool(state models.GPUPoolState) string {
	switch state {
	case models.GPUPoolStateReady:
		return "ready"
	case models.GPUPoolStateProvisioning:
		return "provisioning"
	case models.GPUPoolStateFailed:
		return "failed"
	case models.GPUPoolStateDraining:
		return "draining"
	default:
		return "idle"
	}
}

func readinessFromRuntimes(runtimes []models.AISaaSRuntimeProfile) string {
	readiness := "public-only"
	for _, runtime := range runtimes {
		readiness = bestReadiness(readiness, runtime.Readiness)
	}
	return readiness
}

func bestReadiness(current, next string) string {
	rank := map[string]int{
		"":             0,
		"public-only":  1,
		"configured":   2,
		"idle":         3,
		"draining":     4,
		"provisioning": 5,
		"failed":       6,
		"ready":        7,
		"warning":      8,
	}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func bestLifecycle(current, next string) string {
	rank := map[string]int{
		"":         0,
		"catalog":  1,
		"inactive": 2,
		"active":   3,
		"runtime":  4,
		"unmapped": 5,
	}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func bestPublicStatus(current, next string) string {
	rank := map[string]int{
		"":         0,
		"catalog":  1,
		"inactive": 2,
		"unmapped": 3,
		"active":   4,
	}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func serviceLinks(row *models.AISaaSServiceRecord) []models.AISaaSLink {
	links := []models.AISaaSLink{
		{Label: "Public service", Href: "/services/" + row.Slug, Kind: "public"},
		{Label: "Docs", Href: row.Public.DocsPath, Kind: "public"},
		{Label: "Beta services", Href: "/admin/beta-services", Kind: "admin"},
		{Label: "Beta keys", Href: "/admin/beta-keys", Kind: "admin"},
		{Label: "GPU pools", Href: "/admin/gpu-pools", Kind: "admin"},
		{Label: "Usage analytics", Href: "/admin/services", Kind: "admin"},
		{Label: "Feedback", Href: "/admin/beta-feedback", Kind: "admin"},
	}
	return links
}

func nextSafeAction(row *models.AISaaSServiceRecord) string {
	if len(row.Warnings) > 0 {
		return "Review warnings, then use existing admin pages for edits."
	}
	switch row.Readiness {
	case "failed":
		return "Open GPU pools diagnostics from the existing runtime page."
	case "provisioning":
		return "Watch GPU pool jobs before taking any runtime action."
	default:
		return "Read-only canonical review. Use existing admin pages for writes."
	}
}

func sortedRuntime(runtimes []models.AISaaSRuntimeProfile) []models.AISaaSRuntimeProfile {
	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].RuntimeID < runtimes[j].RuntimeID
	})
	for i := range runtimes {
		sort.Slice(runtimes[i].Jobs, func(a, b int) bool {
			return runtimes[i].Jobs[a].CreatedAt.After(runtimes[i].Jobs[b].CreatedAt)
		})
	}
	return runtimes
}

func sortedWarnings(warnings []models.AISaaSWarning) []models.AISaaSWarning {
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Severity != warnings[j].Severity {
			return warnings[i].Severity < warnings[j].Severity
		}
		if warnings[i].Code != warnings[j].Code {
			return warnings[i].Code < warnings[j].Code
		}
		return warnings[i].Message < warnings[j].Message
	})
	return warnings
}

func appendUnique(values []string, value string) []string {
	normalized := normalizeAISaaSAlias(value)
	if normalized == "" {
		return values
	}
	for _, existing := range values {
		if normalizeAISaaSAlias(existing) == normalized {
			return values
		}
	}
	return append(values, normalized)
}

func sanitizeAISaaSText(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = aiSaaSAWSAccessKeyPattern.ReplaceAllString(value, "<redacted>")
	value = aiSaaSSecretKVPattern.ReplaceAllString(value, "$1=<redacted>")
	value = aiSaaSURLCredentialsPattern.ReplaceAllString(value, "${1}:<redacted>@")
	if limit > 0 && len(value) > limit {
		value = value[:limit] + "..."
	}
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
