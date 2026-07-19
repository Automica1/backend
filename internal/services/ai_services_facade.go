package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

// AIServicesFacade is the apiId-first mutation surface for /admin/ai-services.
// It resolves identity via the explicit alias catalog, audits every mutation,
// and delegates to existing domain services (no new Mongo collections).
type AIServicesFacade interface {
	ListServices(ctx context.Context, startDate, endDate *time.Time) (*models.AISaaSListResponse, error)
	GetService(ctx context.Context, apiID string, startDate, endDate *time.Time) (*models.AISaaSServiceRecord, error)
	ResolveDefinition(apiID string) (AISaaSAliasDefinition, error)
	ResolveGPUServiceTag(apiID, runtimeProfile string) (string, error)
	UpdatePolicy(ctx context.Context, actorEmail, apiID, runtimeProfile string, policy *models.ServicePolicy) (*models.AISaaSServiceRecord, error)
	UpdateRegistry(ctx context.Context, actorEmail, apiID, runtimeProfile string, registry *models.BetaServiceRegistrySettings) (*models.AISaaSServiceRecord, error)
	ListRegistryImages(ctx context.Context, apiID, runtimeProfile, providerOverride, imageOverride string) (*models.RegistryImageCatalog, error)
	UpdateProvision(ctx context.Context, actorEmail, apiID, runtimeProfile string, cfg *models.GPUProvisionConfig, expectedUpdatedAt *time.Time) (*models.GPUProvisionConfig, error)
	IssueBetaKey(ctx context.Context, actorEmail, apiID, runtimeProfile string, req *models.GenerateBetaKeyRequest) (*models.GenerateBetaKeyResponse, error)
	RevokeBetaKey(ctx context.Context, actorEmail, apiID, keyID string) error
	RuntimeAction(ctx context.Context, actorEmail, apiID string, req AIServicesRuntimeActionRequest) (any, error)
	RecordFacadeAudit(ctx context.Context, actorEmail, action, apiID, outcome, reason string, meta map[string]any)
}

type AIServicesRuntimeActionRequest struct {
	Action         string `json:"action"`
	RuntimeProfile string `json:"runtimeProfile,omitempty"`
	Confirm        string `json:"confirm,omitempty"`
	Immediate      bool   `json:"immediate,omitempty"`
	ExtendMinutes  int    `json:"extendMinutes,omitempty"`
	ExpectedState  string `json:"expectedState,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type aiServicesFacade struct {
	resolver     *AISaaSIdentityResolver
	aiSaaS       AISaaSService
	betaServices BetaServiceService
	betaKeys     BetaKeyService
	gpuPools     GPUPoolService
	provision    GPUProvisionConfigService
	admin        AdminService
	gpuPoolRepo  interface {
		GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error)
	}
}

func NewAIServicesFacade(
	aiSaaS AISaaSService,
	betaServices BetaServiceService,
	betaKeys BetaKeyService,
	gpuPools GPUPoolService,
	provision GPUProvisionConfigService,
	admin AdminService,
	gpuPoolGetter interface {
		GetByServiceTag(ctx context.Context, serviceTag string) (*models.GPUPool, error)
	},
) AIServicesFacade {
	return &aiServicesFacade{
		resolver:     NewAISaaSIdentityResolver(),
		aiSaaS:       aiSaaS,
		betaServices: betaServices,
		betaKeys:     betaKeys,
		gpuPools:     gpuPools,
		provision:    provision,
		admin:        admin,
		gpuPoolRepo:  gpuPoolGetter,
	}
}

func (s *aiServicesFacade) ListServices(ctx context.Context, startDate, endDate *time.Time) (*models.AISaaSListResponse, error) {
	return s.aiSaaS.ListServices(ctx, startDate, endDate)
}

func (s *aiServicesFacade) GetService(ctx context.Context, apiID string, startDate, endDate *time.Time) (*models.AISaaSServiceRecord, error) {
	def, err := s.requireDefinition(apiID)
	if err != nil {
		return nil, err
	}
	resp, err := s.aiSaaS.ListServices(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}
	for i := range resp.Services {
		if resp.Services[i].APIID == def.APIID {
			return &resp.Services[i], nil
		}
	}
	return nil, apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, "AI API not found in aggregate")
}

// ResolveDefinition exposes catalog resolution for thin HTTP wrappers
// (feedback scoping, diagnostics delegation).
func (s *aiServicesFacade) ResolveDefinition(apiID string) (AISaaSAliasDefinition, error) {
	return s.requireDefinition(apiID)
}

// ResolveGPUServiceTag resolves the concrete GPU serviceTag owned by apiId.
func (s *aiServicesFacade) ResolveGPUServiceTag(apiID, runtimeProfile string) (string, error) {
	_, tag, err := s.resolveGPUTag(apiID, runtimeProfile)
	return tag, err
}

// RecordFacadeAudit lets HTTP wrappers audit facade-adjacent operations
// (e.g. diagnostics delegated to the legacy GPU probe).
func (s *aiServicesFacade) RecordFacadeAudit(ctx context.Context, actorEmail, action, apiID, outcome, reason string, meta map[string]any) {
	s.audit(ctx, actorEmail, action, apiID, outcome, reason, meta)
}

func (s *aiServicesFacade) UpdatePolicy(ctx context.Context, actorEmail, apiID, runtimeProfile string, policy *models.ServicePolicy) (*models.AISaaSServiceRecord, error) {
	def, tag, err := s.resolveBetaTag(apiID, runtimeProfile)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "servicePolicy is required")
	}
	_, err = s.betaServices.Update(ctx, tag, &models.UpdateBetaServiceRequest{ServicePolicy: policy})
	outcome := "success"
	if err != nil {
		outcome = "error"
		s.audit(ctx, actorEmail, "ai_services.policy.update", def.APIID, outcome, err.Error(), map[string]any{"betaServiceTag": tag})
		return nil, err
	}
	s.audit(ctx, actorEmail, "ai_services.policy.update", def.APIID, outcome, "", map[string]any{"betaServiceTag": tag})
	return s.GetService(ctx, def.APIID, nil, nil)
}

func (s *aiServicesFacade) UpdateRegistry(ctx context.Context, actorEmail, apiID, runtimeProfile string, registry *models.BetaServiceRegistrySettings) (*models.AISaaSServiceRecord, error) {
	def, tag, err := s.resolveBetaTag(apiID, runtimeProfile)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "registrySettings is required")
	}
	// Never persist opaque secret fields from the aggregate path; registry model has no secret echo fields.
	sanitized := *registry
	// One policy: write canonical GPU tag when the profile is a legacy route alias.
	writeTag := tag
	if models.CanonicalGPUServiceTag(tag) != tag {
		for _, candidate := range def.BetaServiceTags {
			if normalizeAISaaSAlias(candidate) == normalizeAISaaSAlias(models.CanonicalGPUServiceTag(tag)) {
				writeTag = candidate
				break
			}
		}
		if writeTag == tag {
			for _, candidate := range def.GPUServiceTags {
				if normalizeAISaaSAlias(candidate) == normalizeAISaaSAlias(models.CanonicalGPUServiceTag(tag)) {
					writeTag = candidate
					break
				}
			}
		}
	}
	_, err = s.betaServices.Update(ctx, writeTag, &models.UpdateBetaServiceRequest{RegistrySettings: &sanitized})
	outcome := "success"
	if err != nil {
		outcome = "error"
		s.audit(ctx, actorEmail, "ai_services.registry.update", def.APIID, outcome, err.Error(), map[string]any{"betaServiceTag": writeTag})
		return nil, err
	}
	// Mirror onto legacy GPU route tags so old beta keys keep the same image policy.
	mirrored := []string{}
	canonical := models.CanonicalGPUServiceTag(writeTag)
	for _, alias := range def.BetaServiceTags {
		if normalizeAISaaSAlias(alias) == normalizeAISaaSAlias(writeTag) {
			continue
		}
		if models.CanonicalGPUServiceTag(alias) != canonical {
			continue
		}
		if _, mirrorErr := s.betaServices.Update(ctx, alias, &models.UpdateBetaServiceRequest{RegistrySettings: &sanitized}); mirrorErr == nil {
			mirrored = append(mirrored, alias)
		}
	}
	s.audit(ctx, actorEmail, "ai_services.registry.update", def.APIID, outcome, "", map[string]any{
		"betaServiceTag": writeTag,
		"mirroredTags":   mirrored,
		"provider":       sanitized.Provider,
		"imageTag":       sanitized.ImageTag,
	})
	return s.GetService(ctx, def.APIID, nil, nil)
}

func (s *aiServicesFacade) UpdateProvision(ctx context.Context, actorEmail, apiID, runtimeProfile string, cfg *models.GPUProvisionConfig, expectedUpdatedAt *time.Time) (*models.GPUProvisionConfig, error) {
	def, tag, err := s.resolveGPUTag(apiID, runtimeProfile)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "provision config is required")
	}
	// Optimistic concurrency: reject when the stored config moved past the
	// revision the caller loaded.
	if expectedUpdatedAt != nil && s.provision != nil {
		current, getErr := s.provision.GetOrDefault(ctx, tag)
		if getErr != nil {
			return nil, getErr
		}
		if current != nil && !current.UpdatedAt.Equal(*expectedUpdatedAt) {
			return nil, apperrors.NewAppError(apperrors.ErrConflict, http.StatusConflict,
				"provision config changed since it was loaded; refetch and retry")
		}
	}
	cfg.ServiceTag = tag
	if strings.TrimSpace(cfg.UpdatedBy) == "" {
		cfg.UpdatedBy = actorEmail
	}
	updated, err := s.provision.Upsert(ctx, cfg)
	outcome := "success"
	if err != nil {
		outcome = "error"
		s.audit(ctx, actorEmail, "ai_services.provision.update", def.APIID, outcome, err.Error(), map[string]any{"gpuServiceTag": tag})
		return nil, err
	}
	s.audit(ctx, actorEmail, "ai_services.provision.update", def.APIID, outcome, "", map[string]any{"gpuServiceTag": tag})
	return updated, nil
}

func (s *aiServicesFacade) IssueBetaKey(ctx context.Context, actorEmail, apiID, runtimeProfile string, req *models.GenerateBetaKeyRequest) (*models.GenerateBetaKeyResponse, error) {
	def, tag, err := s.resolveBetaTag(apiID, runtimeProfile)
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &models.GenerateBetaKeyRequest{}
	}
	if strings.TrimSpace(req.ServiceName) == "" {
		name := ""
		if len(def.BetaServiceNames) > 0 {
			name = def.BetaServiceNames[0]
		}
		req.ServiceName = aiServicesFirstNonEmpty(name, def.Slug, def.APIID)
	}
	if strings.TrimSpace(req.BetaServiceTag) == "" {
		req.BetaServiceTag = tag
	}
	// Authority is apiId resolution — reject client tags that don't match.
	if normalizeAISaaSAlias(req.BetaServiceTag) != normalizeAISaaSAlias(tag) {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, "betaServiceTag does not match resolved apiId")
	}
	resp, err := s.betaKeys.GenerateKey(ctx, req, actorEmail)
	outcome := "success"
	if err != nil {
		outcome = "error"
		s.audit(ctx, actorEmail, "ai_services.access.key.issue", def.APIID, outcome, err.Error(), map[string]any{"betaServiceTag": tag})
		return nil, err
	}
	s.audit(ctx, actorEmail, "ai_services.access.key.issue", def.APIID, outcome, "", map[string]any{"betaServiceTag": tag})
	return resp, nil
}

func (s *aiServicesFacade) RevokeBetaKey(ctx context.Context, actorEmail, apiID, keyID string) error {
	def, err := s.requireDefinition(apiID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(keyID) == "" {
		return apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "keyId is required")
	}

	// Ownership gate: the key must belong to this API's alias catalog before
	// it can be revoked through the facade.
	tag := ""
	if s.betaKeys != nil {
		keys, listErr := s.betaKeys.ListKeys(ctx, "")
		if listErr != nil {
			return listErr
		}
		var target *models.BetaKey
		for _, key := range keys {
			if key != nil && key.ID.Hex() == keyID {
				target = key
				break
			}
		}
		if target == nil {
			return apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, "beta key not found")
		}
		if !betaKeyBelongsToDefinition(target, def) {
			return apperrors.NewAppError(apperrors.ErrConflict, http.StatusConflict, "beta key does not belong to apiId "+def.APIID)
		}
		tag = target.BetaServiceTag
	}

	_, err = s.betaKeys.RevokeKey(ctx, keyID)
	outcome := "success"
	if err != nil {
		outcome = "error"
		s.audit(ctx, actorEmail, "ai_services.access.key.revoke", def.APIID, outcome, err.Error(), map[string]any{"betaServiceTag": tag, "keyId": keyID})
		return err
	}
	s.audit(ctx, actorEmail, "ai_services.access.key.revoke", def.APIID, outcome, "", map[string]any{"betaServiceTag": tag, "keyId": keyID})
	return nil
}

func (s *aiServicesFacade) RuntimeAction(ctx context.Context, actorEmail, apiID string, req AIServicesRuntimeActionRequest) (any, error) {
	def, tag, err := s.resolveGPUTag(apiID, req.RuntimeProfile)
	if err != nil {
		return nil, err
	}
	action := strings.TrimSpace(strings.ToLower(req.Action))
	if action == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "action is required")
	}

	if s.gpuPoolRepo != nil && req.ExpectedState != "" {
		if pool, _ := s.gpuPoolRepo.GetByServiceTag(ctx, tag); pool != nil {
			if string(pool.State) != req.ExpectedState {
				return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict,
					fmt.Sprintf("runtime state changed: expected %s, got %s", req.ExpectedState, pool.State))
			}
		}
	}

	var result any
	switch action {
	case "warm-start", "start":
		result, err = s.gpuPools.AdminWarmStart(ctx, tag)
	case "recover":
		result, err = s.gpuPools.AdminRecover(ctx, tag)
	case "retry-provision", "retry":
		err = s.gpuPools.AdminRetryProvision(ctx, tag)
		result = map[string]any{"ok": err == nil}
	case "abort-provision", "abort":
		err = s.gpuPools.AdminAbortProvision(ctx, tag)
		result = map[string]any{"ok": err == nil}
	case "grace-stop", "grace", "shutdown":
		if req.Immediate {
			return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "use action=destroy for immediate shutdown")
		}
		result, err = s.gpuPools.AdminShutdown(ctx, tag, false)
	case "cancel-grace":
		result, err = s.gpuPools.AdminCancelGrace(ctx, tag)
	case "extend-grace":
		mins := req.ExtendMinutes
		if mins <= 0 {
			mins = 5
		}
		result, err = s.gpuPools.AdminExtendGrace(ctx, tag, time.Duration(mins)*time.Minute)
	case "destroy", "shutdown-immediate":
		if strings.TrimSpace(req.Confirm) != "DESTROY" {
			return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, `confirm must equal "DESTROY"`)
		}
		if s.gpuPoolRepo != nil {
			if pool, _ := s.gpuPoolRepo.GetByServiceTag(ctx, tag); pool != nil {
				if pool.RefCount > 0 || activeSessionCount(pool) > 0 {
					msg := "cannot destroy runtime while sessions are active; stop sessions first"
					s.audit(ctx, actorEmail, "ai_services.runtime."+action, def.APIID, "error", msg, map[string]any{
						"gpuServiceTag":  tag,
						"runtimeProfile": aiServicesFirstNonEmpty(req.RuntimeProfile, tag),
						"immediate":      true,
					})
					return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, msg)
				}
			}
		}
		result, err = s.gpuPools.AdminShutdown(ctx, tag, true)
	default:
		return nil, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "unsupported runtime action")
	}

	outcome := "success"
	reason := req.Reason
	if err != nil {
		outcome = "error"
		if reason == "" {
			reason = err.Error()
		}
	}
	s.audit(ctx, actorEmail, "ai_services.runtime."+action, def.APIID, outcome, reason, map[string]any{
		"gpuServiceTag":  tag,
		"runtimeProfile": aiServicesFirstNonEmpty(req.RuntimeProfile, tag),
		"immediate":      req.Immediate,
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *aiServicesFacade) poolSnapshot(ctx context.Context, tag string) *models.GPUPool {
	if s.gpuPoolRepo == nil {
		return nil
	}
	pool, _ := s.gpuPoolRepo.GetByServiceTag(ctx, tag)
	return pool
}

func (s *aiServicesFacade) requireDefinition(apiID string) (AISaaSAliasDefinition, error) {
	def, ok := s.resolver.Definition(apiID)
	if !ok {
		return AISaaSAliasDefinition{}, apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, "unknown apiId")
	}
	return def, nil
}

// resolveBetaTag resolves the beta_services tag owned by apiId. An optional
// runtimeProfile disambiguates when the catalog lists several beta tags; a
// profile outside the catalog is a conflict, never an authority.
func (s *aiServicesFacade) resolveBetaTag(apiID, runtimeProfile string) (AISaaSAliasDefinition, string, error) {
	if runtimeProfile == "" {
		return s.resolveSingleBetaTag(apiID)
	}
	def, err := s.requireDefinition(apiID)
	if err != nil {
		return def, "", err
	}
	tags := uniqueSortedStrings(def.BetaServiceTags)
	if len(tags) == 0 {
		tags = uniqueSortedStrings(def.GPUServiceTags)
	}
	want := normalizeAISaaSAlias(runtimeProfile)
	for _, tag := range tags {
		if normalizeAISaaSAlias(tag) == want {
			return def, tag, nil
		}
	}
	return def, "", apperrors.NewAppError(apperrors.ErrConflict, http.StatusConflict, "runtimeProfile does not belong to apiId")
}

func (s *aiServicesFacade) resolveSingleBetaTag(apiID string) (AISaaSAliasDefinition, string, error) {
	def, err := s.requireDefinition(apiID)
	if err != nil {
		return def, "", err
	}
	tags := uniqueSortedStrings(def.BetaServiceTags)
	if len(tags) == 0 {
		// Fall back to GPU tag as beta tag when catalogs use the same string.
		tags = uniqueSortedStrings(def.GPUServiceTags)
	}
	if len(tags) == 0 {
		return def, "", apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, "no betaServiceTag alias for apiId")
	}
	if len(tags) > 1 {
		return def, "", apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, "ambiguous betaServiceTag aliases for apiId")
	}
	return def, tags[0], nil
}

func (s *aiServicesFacade) resolveGPUTag(apiID, runtimeProfile string) (AISaaSAliasDefinition, string, error) {
	def, err := s.requireDefinition(apiID)
	if err != nil {
		return def, "", err
	}
	tags := uniqueSortedStrings(def.GPUServiceTags)
	if runtimeProfile != "" {
		want := normalizeAISaaSAlias(runtimeProfile)
		for _, tag := range tags {
			if normalizeAISaaSAlias(tag) == want {
				return def, tag, nil
			}
		}
		// Also allow gpu: prefix from aggregate runtime IDs.
		trimmed := strings.TrimPrefix(want, "gpu:")
		for _, tag := range tags {
			if normalizeAISaaSAlias(tag) == trimmed {
				return def, tag, nil
			}
		}
		// Legacy alias: vlm-e2e-gpu → canonical vlm-gpu (same product, new pool identity).
		if normalizeAISaaSAlias(models.CanonicalGPUServiceTag(trimmed)) != trimmed {
			canonical := normalizeAISaaSAlias(models.CanonicalGPUServiceTag(trimmed))
			for _, tag := range tags {
				if normalizeAISaaSAlias(tag) == canonical {
					return def, tag, nil
				}
			}
		}
		return def, "", apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, "runtimeProfile does not belong to apiId")
	}
	if len(tags) == 0 {
		return def, "", apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, "no gpuServiceTag alias for apiId")
	}
	if len(tags) > 1 {
		return def, "", apperrors.NewAppError(apperrors.ErrValidation, http.StatusConflict, "ambiguous gpuServiceTag aliases; pass runtimeProfile")
	}
	return def, tags[0], nil
}

func (s *aiServicesFacade) audit(ctx context.Context, actorEmail, action, apiID, outcome, reason string, meta map[string]any) {
	if s.admin == nil {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["apiId"] = apiID
	_ = s.admin.RecordAction(ctx, &models.AdminAuditLog{
		ActorEmail: actorEmail,
		Action:     action,
		TargetType: "ai_api",
		TargetID:   apiID,
		Outcome:    outcome,
		Reason:     reason,
		Metadata:   meta,
		Timestamp:  time.Now().UTC(),
	})
}

// betaKeyBelongsToDefinition is the ownership check for key revocation: the
// key's tag or service name must appear in the API's alias catalog.
func betaKeyBelongsToDefinition(key *models.BetaKey, def AISaaSAliasDefinition) bool {
	if key == nil {
		return false
	}
	tag := normalizeAISaaSAlias(key.BetaServiceTag)
	for _, candidate := range def.BetaServiceTags {
		if normalizeAISaaSAlias(candidate) == tag {
			return true
		}
	}
	name := normalizeAISaaSAlias(key.ServiceName)
	for _, candidate := range def.BetaServiceNames {
		if normalizeAISaaSAlias(candidate) == name {
			return true
		}
	}
	return false
}

func activeSessionCount(pool *models.GPUPool) int {
	if pool == nil {
		return 0
	}
	n := 0
	for _, sess := range pool.Sessions {
		if sess.StoppedAt == nil {
			n++
		}
	}
	return n
}

func aiServicesFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
