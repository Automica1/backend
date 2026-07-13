package services

import (
	"context"
	"fmt"
	"strings"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
)

type GPUProvisionConfigService interface {
	GetOrDefault(ctx context.Context, serviceTag string) (*models.GPUProvisionConfig, error)
	Upsert(ctx context.Context, cfg *models.GPUProvisionConfig) (*models.GPUProvisionConfig, error)
	Validate(cfg *models.GPUProvisionConfig) error
	WorkerEnv(cfg *models.GPUProvisionConfig) []string
	EffectiveSummary(cfg *models.GPUProvisionConfig) string
}

type gpuProvisionConfigService struct {
	repo repository.GPUProvisionConfigRepository
}

func NewGPUProvisionConfigService(repo repository.GPUProvisionConfigRepository) GPUProvisionConfigService {
	return &gpuProvisionConfigService{repo: repo}
}

func (s *gpuProvisionConfigService) GetOrDefault(ctx context.Context, serviceTag string) (*models.GPUProvisionConfig, error) {
	cfg, err := s.repo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		cfg.Normalize()
		return cfg, nil
	}
	serviceName := models.ServiceTagToPipelineService[serviceTag]
	if serviceName == "" {
		serviceName = "sign_verify_vlm_gpu"
	}
	out := models.DefaultGPUProvisionConfig(serviceTag, serviceName)
	return out, nil
}

func (s *gpuProvisionConfigService) Upsert(ctx context.Context, cfg *models.GPUProvisionConfig) (*models.GPUProvisionConfig, error) {
	if cfg.ServiceTag == "" {
		return nil, fmt.Errorf("serviceTag is required")
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = models.ServiceTagToPipelineService[cfg.ServiceTag]
	}
	cfg.Normalize()
	if err := s.Validate(cfg); err != nil {
		return nil, err
	}
	return s.repo.Upsert(ctx, cfg)
}

func (s *gpuProvisionConfigService) Validate(cfg *models.GPUProvisionConfig) error {
	if cfg == nil {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "config is required")
	}
	cfg.Normalize()
	inf := cfg.Infrastructure
	if inf.PrimaryProvider == models.GPUProviderGCP || inf.FallbackProvider == models.GPUProviderGCP {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "GCP provider is not available yet")
	}
	t := cfg.Timeouts
	if t.SSHReadyPrimarySec < 60 || t.SSHReadyPrimarySec > 1800 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "sshReadyPrimarySec must be 60–1800")
	}
	if t.SSHReadyFallbackSec < 60 || t.SSHReadyFallbackSec > 900 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "sshReadyFallbackSec must be 60–900")
	}
	if t.E2EWaitSec < 120 || t.E2EWaitSec > 2400 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "e2eWaitSec must be 120–2400")
	}
	if t.E2EStallSec < 60 || t.E2EStallSec > 900 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "e2eStallSec must be 60–900")
	}
	if t.E2EDestroyWaitSec < 60 || t.E2EDestroyWaitSec > 600 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "e2eDestroyWaitSec must be 60–600")
	}
	if t.DeployHealthSec < 30 || t.DeployHealthSec > 600 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "deployHealthSec must be 30–600")
	}
	r := cfg.Retries
	if r.ProvisionMaxAttempts < 1 || r.ProvisionMaxAttempts > 5 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "provisionMaxAttempts must be 1–5")
	}
	if r.DestroyMaxAttempts < 1 || r.DestroyMaxAttempts > 5 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "destroyMaxAttempts must be 1–5")
	}
	l := cfg.Lifecycle
	if l.GracePeriodMin < 1 || l.GracePeriodMin > 60 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "gracePeriodMin must be 1–60")
	}
	if l.ReconnectCooldownSec < 60 || l.ReconnectCooldownSec > 1800 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "reconnectCooldownSec must be 60–1800")
	}
	minNoJob := cfg.MinStuckProvisionNoJobMin()
	if l.StuckProvisionNoJobMin < minNoJob {
		return apperrors.NewAppError(apperrors.ErrValidation, 400,
			fmt.Sprintf("stuckProvisionNoJobMin must be at least %d", minNoJob))
	}
	if l.StuckProvisionZombieMin <= l.StuckProvisionNoJobMin {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "stuckProvisionZombieMin must exceed stuckProvisionNoJobMin")
	}
	if l.UserRetryHintMin < 1 || l.UserRetryHintMin > 120 {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "userRetryHintMin must be 1–120")
	}
	if inf.AWS.InstanceType != "" && inf.AWS.InstanceType != "g6.xlarge" && inf.AWS.InstanceType != "g6.2xlarge" {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "instanceType must be g6.xlarge or g6.2xlarge")
	}
	return nil
}

func (s *gpuProvisionConfigService) WorkerEnv(cfg *models.GPUProvisionConfig) []string {
	if cfg == nil {
		return nil
	}
	cfg.Normalize()
	inf := cfg.Infrastructure
	t := cfg.Timeouts
	fallback := string(inf.FallbackProvider)
	if fallback == "" {
		fallback = "aws"
	}
	reuse := "0"
	if cfg.Retries.ReuseNodeOnRetry {
		reuse = "1"
	}
	lines := []string{
		"GPU_PROVISION_PRIMARY=" + string(inf.PrimaryProvider),
		"GPU_PROVISION_FALLBACK=" + fallback,
		fmt.Sprintf("GPU_SSH_READY_TIMEOUT_SEC=%d", t.SSHReadyPrimarySec),
		fmt.Sprintf("GPU_FALLBACK_SSH_READY_TIMEOUT_SEC=%d", t.SSHReadyFallbackSec),
		fmt.Sprintf("E2E_WAIT_TIMEOUT_SEC=%d", t.E2EWaitSec),
		fmt.Sprintf("E2E_STALL_SEC=%d", t.E2EStallSec),
		fmt.Sprintf("E2E_DESTROY_WAIT_SEC=%d", t.E2EDestroyWaitSec),
		fmt.Sprintf("GPU_DEPLOY_HEALTH_TIMEOUT_SEC=%d", t.DeployHealthSec),
		"GPU_RETRY_REUSE_NODE=" + reuse,
		"GPU_SERVICE_TAG=" + cfg.ServiceTag,
	}
	aws := inf.AWS
	if aws.Region != "" {
		lines = append(lines, "AWS_REGION="+aws.Region)
	}
	if aws.InstanceType != "" {
		lines = append(lines, "AWS_INSTANCE_TYPE="+aws.InstanceType)
	}
	if aws.CapacityType != "" {
		lines = append(lines, "AWS_CAPACITY_TYPE="+aws.CapacityType)
	}
	if aws.CapacityFallback != "" {
		lines = append(lines, "AWS_CAPACITY_FALLBACK="+aws.CapacityFallback)
	}
	if aws.FallbackCapacity != "" {
		lines = append(lines, "AWS_FALLBACK_CAPACITY_TYPE="+aws.FallbackCapacity)
	}
	if aws.SubnetID != "" {
		lines = append(lines, "AWS_SUBNET_ID="+aws.SubnetID)
	}
	if aws.SecurityGroupID != "" {
		lines = append(lines, "AWS_SECURITY_GROUP_ID="+aws.SecurityGroupID)
	}
	e2e := inf.E2E
	if e2e.Location != "" {
		lines = append(lines, "E2E_LOCATION="+e2e.Location)
	}
	if e2e.GPUCard != "" {
		lines = append(lines, "E2E_GPU_CARD="+e2e.GPUCard)
	}
	if e2e.Plan != "" {
		lines = append(lines, "E2E_PLAN="+e2e.Plan)
	}
	return lines
}

func (s *gpuProvisionConfigService) EffectiveSummary(cfg *models.GPUProvisionConfig) string {
	if cfg == nil {
		return ""
	}
	cfg.Normalize()
	inf := cfg.Infrastructure
	fb := string(inf.FallbackProvider)
	if fb == "" {
		fb = "aws"
	}
	capacity := providerCapacitySummary(cfg)
	return fmt.Sprintf("Next cold start: %s → %s · SSH %ds/%ds · %s · %d provision retries",
		displayProvider(inf.PrimaryProvider), displayProvider(models.GPUProviderName(fb)),
		cfg.Timeouts.SSHReadyPrimarySec, cfg.Timeouts.SSHReadyFallbackSec,
		capacity,
		cfg.Retries.ProvisionMaxAttempts,
	)
}

func displayProvider(p models.GPUProviderName) string {
	switch p {
	case models.GPUProviderAWS:
		return "AWS"
	case models.GPUProviderGCP:
		return "GCP"
	case models.GPUProviderName("none"):
		return "none"
	default:
		return "E2E Networks"
	}
}

func providerCapacitySummary(cfg *models.GPUProvisionConfig) string {
	inf := cfg.Infrastructure
	switch inf.PrimaryProvider {
	case models.GPUProviderAWS:
		cap := inf.AWS.CapacityType
		if cap == "" {
			cap = "spot"
		}
		it := inf.AWS.InstanceType
		if it == "" {
			it = "g6.xlarge"
		}
		return fmt.Sprintf("%s %s", it, cap)
	case models.GPUProviderE2E:
		loc := inf.E2E.Location
		if loc == "" {
			loc = "Delhi"
		}
		card := inf.E2E.GPUCard
		if card == "" {
			card = "L4"
		}
		return fmt.Sprintf("%s %s", loc, card)
	default:
		return string(inf.PrimaryProvider)
	}
}

func NormalizeGPUProvider(p string) models.GPUProviderName {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "aws":
		return models.GPUProviderAWS
	case "gcp":
		return models.GPUProviderGCP
	case "none":
		return models.GPUProviderName("none")
	default:
		return models.GPUProviderE2E
	}
}
