package services

import (
	"strings"
	"testing"

	"chi-mongo-backend/internal/models"
)

func TestWorkerEnvMapsPolicyFields(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultGPUProvisionConfig("vlm-e2e-gpu", "sign_verify_vlm_gpu")
	cfg.Timeouts.SSHReadyPrimarySec = 120
	cfg.Timeouts.E2EWaitSec = 900
	cfg.Retries.ReuseNodeOnRetry = false
	svc := NewGPUProvisionConfigService(nil)
	env := svc.WorkerEnv(cfg)
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"GPU_SSH_READY_TIMEOUT_SEC=120",
		"E2E_WAIT_TIMEOUT_SEC=900",
		"GPU_RETRY_REUSE_NODE=0",
		"GPU_PROVISION_PRIMARY=e2e",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("WorkerEnv missing %q in:\n%s", want, joined)
		}
	}
}

func TestEffectiveSummaryE2EPrimary(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultGPUProvisionConfig("vlm-e2e-gpu", "sign_verify_vlm_gpu")
	svc := NewGPUProvisionConfigService(nil)
	got := svc.EffectiveSummary(cfg)
	if !strings.Contains(got, "E2E Networks") || !strings.Contains(got, "Delhi") {
		t.Fatalf("expected E2E Networks summary, got %q", got)
	}
}

func TestEffectiveSummaryAWSPrimary(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultGPUProvisionConfig("vlm-e2e-gpu", "sign_verify_vlm_gpu")
	cfg.Infrastructure.PrimaryProvider = models.GPUProviderAWS
	cfg.Infrastructure.FallbackProvider = models.GPUProviderName("none")
	svc := NewGPUProvisionConfigService(nil)
	got := svc.EffectiveSummary(cfg)
	if !strings.Contains(got, "g6.xlarge") || strings.Contains(got, "Delhi L4") {
		t.Fatalf("expected AWS instance summary, got %q", got)
	}
}

func TestValidateStuckProvisionUsesPrimaryTimeout(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultGPUProvisionConfig("vlm-e2e-gpu", "sign_verify_vlm_gpu")
	cfg.Infrastructure.PrimaryProvider = models.GPUProviderAWS
	cfg.Infrastructure.FallbackProvider = models.GPUProviderName("none")
	cfg.Timeouts.SSHReadyPrimarySec = 600
	cfg.Timeouts.E2EWaitSec = 600
	cfg.Lifecycle.StuckProvisionNoJobMin = 10
	svc := NewGPUProvisionConfigService(nil)
	if err := svc.Validate(cfg); err == nil {
		t.Fatal("expected validation error when stuckProvisionNoJobMin too low for AWS primary")
	}
}

func TestMinStuckProvisionNoJobMinAWS(t *testing.T) {
	t.Parallel()
	cfg := models.DefaultGPUProvisionConfig("vlm-e2e-gpu", "sign_verify_vlm_gpu")
	cfg.Infrastructure.PrimaryProvider = models.GPUProviderAWS
	cfg.Infrastructure.FallbackProvider = models.GPUProviderName("none")
	cfg.Timeouts.SSHReadyPrimarySec = 600
	min := cfg.MinStuckProvisionNoJobMin()
	if min != 12 {
		t.Fatalf("expected 12, got %d", min)
	}
}
