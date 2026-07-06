package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/internal/services"
	"chi-mongo-backend/internal/worker"
)

type jobLog struct {
	path string
	file *os.File
}

func (h *GPUPoolHandlers) openJobLog(job *models.Job) (*jobLog, error) {
	root := h.automicaRoot()
	dir := filepath.Join(root, "logs", "worker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create worker log dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-%s.log", job.Type, job.ID.Hex()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open worker log: %w", err)
	}

	header := fmt.Sprintf(
		"=== automica-worker job=%s type=%s attempt=%d/%d started=%s ===\n",
		job.ID.Hex(), job.Type, job.Attempts, job.MaxAttempts, time.Now().UTC().Format(time.RFC3339),
	)
	if _, err := file.WriteString(header); err != nil {
		_ = file.Close()
		return nil, err
	}

	latest := filepath.Join(dir, "latest.log")
	_ = os.Remove(latest)
	_ = os.Symlink(filepath.Base(path), latest)

	log.Printf("job log: %s (live: tail -f %s)", path, path)
	return &jobLog{path: path, file: file}, nil
}

func (jl *jobLog) writeLine(prefix, msg string) {
	if jl == nil || jl.file == nil || msg == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		_, _ = fmt.Fprintf(jl.file, "[%s] %s\n", prefix, line)
	}
	_ = jl.file.Sync()
}

func (jl *jobLog) close() {
	if jl == nil || jl.file == nil {
		return
	}
	_, _ = fmt.Fprintf(jl.file, "=== finished %s ===\n", time.Now().UTC().Format(time.RFC3339))
	_ = jl.file.Close()
	jl.file = nil
}

type GPUPoolHandlers struct {
	cfg           *config.Config
	poolRepo      repository.GPUPoolRepository
	gpuPoolService services.GPUPoolService
}

func NewGPUPoolHandlers(cfg *config.Config, poolRepo repository.GPUPoolRepository, gpuPoolService services.GPUPoolService) *GPUPoolHandlers {
	return &GPUPoolHandlers{cfg: cfg, poolRepo: poolRepo, gpuPoolService: gpuPoolService}
}

func (h *GPUPoolHandlers) Register(registry *worker.Registry) {
	registry.Register(models.JobTypeGPUPoolProvision, h.handleProvision)
	registry.Register(models.JobTypeGPUPoolDestroy, h.handleDestroy)
	registry.Register(models.JobTypeGPUPoolGraceDestroy, h.handleGraceDestroy)
	registry.Register(models.JobTypeGPUPoolHealthCheck, h.handleHealthCheck)
	registry.Register(models.JobTypeGPUPoolMeterTick, h.handleMeterTick)
}

func (h *GPUPoolHandlers) automicaRoot() string {
	if h.cfg.Worker.AutomicaRoot != "" {
		return h.cfg.Worker.AutomicaRoot
	}
	if v := os.Getenv("AUTOMICA_ROOT"); v != "" {
		return v
	}
	return "."
}

// e2eOnlyEnv: option-1 GPU pool path — build on E2E GPU (SKIP_LOCAL_PREP); Ollama on E2E only.
// Shorter E2E timeouts for user-facing Start Testing (fail fast vs Mac manual 20m wait).
func (h *GPUPoolHandlers) e2eOnlyEnv(root string, job *models.Job) []string {
	target := os.Getenv("AUTOMICA_TARGET")
	if target == "" {
		target = "dev2"
	}
	env := []string{
		"AUTOMICA_ROOT=" + root,
		"AUTOMICA_TARGET=" + target,
		"ROOT=" + root,
		"SKIP_LOCAL_PREP=1",          // no dev2 docker tar — E2E pulls base image natively
		"SKIP_OLLAMA_OFFLINE_PREP=1", // large model pulled on E2E GPU only
		"E2E_USE_SAVED_IMAGE=0",
		"E2E_WAIT_TIMEOUT_SEC=600",  // 10 min max wait-for-running (Mac manual default 1200)
		"E2E_STALL_SEC=360",         // 6 min unchanged Creating/Deleting → fail
		"E2E_DESTROY_WAIT_SEC=180",  // 3 min max wait for delete during --fresh teardown
		"GPU_POOL_SYNC_NODE_OWNER=user",
	}
	if job != nil {
		env = append(env, fmt.Sprintf("WORKER_JOB_ATTEMPT=%d", job.Attempts))
	}
	return env
}

type provisionState struct {
	NodeID           string `json:"node_id"`
	PublicIP         string `json:"public_ip"`
	PreviousPublicIP string `json:"previous_public_ip"`
	Status           string `json:"status"`
}

func (h *GPUPoolHandlers) provisionStatePath() string {
	return filepath.Join(h.automicaRoot(), "services/pipeline/registry/.e2e_provision_state.json")
}

func (h *GPUPoolHandlers) clearProvisionState() {
	_ = os.Remove(h.provisionStatePath())
}

func (h *GPUPoolHandlers) e2eNodeListed(nodeID string) bool {
	if nodeID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := h.automicaRoot()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts/e2e_provision_node.sh"), "list-nodes")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("provision state check: list-nodes failed: %v", err)
		return false
	}
	return strings.Contains(string(out), nodeID)
}

func provisionStateReusable(st *provisionState) bool {
	if st == nil || st.PublicIP == "" {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(st.Status))
	if s == "" || s == "running" {
		return true
	}
	switch {
	case strings.HasPrefix(s, "failed"),
		strings.Contains(s, "failed"),
		strings.HasPrefix(s, "error"),
		strings.Contains(s, "error"),
		s == "terminated", s == "deleted", s == "cancelled", s == "stopped":
		return false
	default:
		// Creating/Starting/etc. — retry with fresh teardown, not reuse.
		return false
	}
}

func (h *GPUPoolHandlers) bootstrapArgsForJob(job *models.Job, serviceName string) []string {
	args := []string{serviceName}
	if job.Attempts > 1 {
		if st, err := h.readProvisionState(); err == nil && provisionStateReusable(st) && h.e2eNodeListed(st.NodeID) {
			log.Printf("provision retry attempt=%d/%d: reusing node ip=%s status=%s", job.Attempts, job.MaxAttempts, st.PublicIP, st.Status)
			return append(args, "--reuse-node")
		}
		h.clearProvisionState()
		log.Printf("provision retry attempt=%d/%d: fresh create (no live node in E2E)", job.Attempts, job.MaxAttempts)
	}
	return append(args, "--fresh")
}

func (h *GPUPoolHandlers) runScript(ctx context.Context, jl *jobLog, job *models.Job, script string, args ...string) error {
	return h.runScriptWithExtraEnv(ctx, jl, job, nil, script, args...)
}

func (h *GPUPoolHandlers) runScriptWithExtraEnv(ctx context.Context, jl *jobLog, job *models.Job, extraEnv []string, script string, args ...string) error {
	root := h.automicaRoot()
	cmdPath := filepath.Join(root, script)
	cmd := exec.CommandContext(ctx, "bash", append([]string{cmdPath}, args...)...)
	cmd.Dir = root
	baseEnv := h.e2eOnlyEnv(root, job)
	if len(extraEnv) > 0 {
		baseEnv = append(baseEnv, extraEnv...)
	}
	cmd.Env = append(os.Environ(), baseEnv...)

	var stdout, stderr bytes.Buffer
	stdoutWriters := []io.Writer{&stdout}
	stderrWriters := []io.Writer{&stderr}
	if jl != nil && jl.file != nil {
		stdoutWriters = append(stdoutWriters, jl.file)
		stderrWriters = append(stderrWriters, jl.file)
	}
	cmd.Stdout = io.MultiWriter(stdoutWriters...)
	cmd.Stderr = io.MultiWriter(stderrWriters...)

	execLine := fmt.Sprintf("exec: bash %s %s", script, strings.Join(args, " "))
	log.Print(execLine)
	if jl != nil {
		jl.writeLine("worker", execLine)
	}

	err := cmd.Run()
	if stdout.Len() > 0 {
		log.Printf("stdout:\n%s", stdout.String())
	}
	if stderr.Len() > 0 {
		log.Printf("stderr:\n%s", stderr.String())
	}
	if err != nil {
		return fmt.Errorf("%s: %w\nstderr: %s", script, err, stderr.String())
	}
	return nil
}

func (h *GPUPoolHandlers) runPipeline(ctx context.Context, jl *jobLog, job *models.Job, serviceName, stages string) error {
	root := h.automicaRoot()
	pipeline := filepath.Join(root, "services/pipeline/run_pipeline.sh")
	cmd := exec.CommandContext(ctx, "bash", pipeline, serviceName)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), append(h.e2eOnlyEnv(root, job),
		"PIPELINE_STAGE="+stages,
		"PIPELINE_PROFILE=beta",
	)...)

	var out bytes.Buffer
	writers := []io.Writer{&out}
	if jl != nil && jl.file != nil {
		writers = append(writers, jl.file)
	}
	cmd.Stdout = io.MultiWriter(writers...)
	cmd.Stderr = io.MultiWriter(writers...)

	execLine := fmt.Sprintf("exec: pipeline %s stages=%s", serviceName, stages)
	log.Print(execLine)
	if jl != nil {
		jl.writeLine("worker", execLine)
	}

	err := cmd.Run()
	if out.Len() > 0 {
		log.Printf("pipeline stdout:\n%s", out.String())
	}
	if err != nil {
		return fmt.Errorf("pipeline %s: %w\n%s", stages, err, out.String())
	}
	return nil
}

func (h *GPUPoolHandlers) readProvisionState() (*provisionState, error) {
	root := h.automicaRoot()
	statePath := filepath.Join(root, "services/pipeline/registry/.e2e_provision_state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}
	var st provisionState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (h *GPUPoolHandlers) payloadString(job *models.Job, key string) string {
	if job.Payload == nil {
		return ""
	}
	v, ok := job.Payload[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

func (h *GPUPoolHandlers) markProvisionFailed(ctx context.Context, serviceTag, errMsg string) {
	_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":     models.GPUPoolStateFailed,
		"lastError": services.SanitizeGPUPoolUserError(errMsg),
	})
	if err != nil {
		log.Printf("mark provision failed for %s: %v", serviceTag, err)
		return
	}
	if err := h.gpuPoolService.HandleProvisionFailed(ctx, serviceTag); err != nil {
		log.Printf("refund failed pool sessions for %s: %v", serviceTag, err)
	}
}

func (h *GPUPoolHandlers) failProvisionIfFinal(ctx context.Context, job *models.Job, serviceTag string, err error) error {
	if err != nil && (job.Attempts >= job.MaxAttempts || services.IsTerminalProvisionError(err.Error())) {
		h.markProvisionFailed(ctx, serviceTag, err.Error())
	}
	return err
}

func (h *GPUPoolHandlers) handleProvision(ctx context.Context, job *models.Job) error {
	jl, err := h.openJobLog(job)
	if err != nil {
		log.Printf("job log unavailable: %v", err)
	} else {
		defer jl.close()
	}

	serviceTag := h.payloadString(job, "serviceTag")
	serviceName := h.payloadString(job, "serviceName")
	if serviceTag == "" || serviceName == "" {
		return fmt.Errorf("provision job missing serviceTag/serviceName")
	}

	pool, err := h.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}
	if pool == nil {
		err = fmt.Errorf("gpu pool not found: %s", serviceTag)
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}
	if pool.RefCount == 0 {
		log.Printf("provision skipped: refCount=0 for %s", serviceTag)
		if err := h.gpuPoolService.OnProvisionSkippedNoSessions(ctx, serviceTag); err != nil {
			log.Printf("provision skip handling for %s: %v", serviceTag, err)
		}
		return nil
	}

	// Option 1: full bootstrap on E2E. Retries reuse an existing node when deploy failed mid-flight.
	bootstrapArgs := h.bootstrapArgsForJob(job, serviceName)
	if err := h.runScript(ctx, jl, job, "scripts/e2e_bootstrap.sh", bootstrapArgs...); err != nil {
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}

	// Gateway + beta registry + Mongo pool ready (same stages as Mac bootstrap pipeline).
	if err := h.runPipeline(ctx, jl, job, serviceName, "gateway,gateway-smoke,beta-seed,gpu-pool-sync"); err != nil {
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}

	if err := h.runScript(ctx, jl, job, "scripts/e2e_ensure_firewall.sh", serviceName); err != nil {
		log.Printf("firewall hook warning: %v", err)
	}

	return nil
}

func (h *GPUPoolHandlers) handleDestroy(ctx context.Context, job *models.Job) error {
	jl, err := h.openJobLog(job)
	if err != nil {
		log.Printf("job log unavailable: %v", err)
	} else {
		defer jl.close()
	}

	serviceTag := h.payloadString(job, "serviceTag")
	var destroyErr error
	if serviceTag != "" {
		pool, poolErr := h.poolRepo.GetByServiceTag(ctx, serviceTag)
		if poolErr != nil {
			return poolErr
		}
		switch {
		case pool != nil && pool.NodeID != "":
			// Mac bootstrap writes nodeId to Mongo via gpu-pool-sync; dev2 has no local state file.
			log.Printf("destroy: Mongo nodeId=%s serviceTag=%s", pool.NodeID, serviceTag)
			destroyErr = h.runScript(ctx, jl, job, "scripts/e2e_provision_node.sh", "destroy", pool.NodeID)
		case pool != nil && pool.PublicIP != "":
			log.Printf("destroy: resolve by publicIp=%s serviceTag=%s", pool.PublicIP, serviceTag)
			destroyErr = h.runScriptWithExtraEnv(ctx, jl, job,
				[]string{"E2E_DESTROY_PUBLIC_IP=" + pool.PublicIP},
				"scripts/e2e_provision_node.sh", "destroy")
		default:
			destroyErr = h.runScript(ctx, jl, job, "scripts/e2e_bootstrap.sh", "--destroy")
		}
	} else {
		destroyErr = h.runScript(ctx, jl, job, "scripts/e2e_bootstrap.sh", "--destroy")
	}
	if destroyErr != nil {
		log.Printf("destroy script warning (continuing to reset pool state): %v", destroyErr)
	}
	if serviceTag != "" {
		_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
			"state":          models.GPUPoolStateIdle,
			"refCount":       0,
			"nodeId":         "",
			"publicIp":       "",
			"drainStartedAt": nil,
			"drainReason":    "",
			"readyAt":        nil,
			"lastError":      "",
		})
		if err != nil {
			return err
		}
		return nil
	}
	return destroyErr
}

func (h *GPUPoolHandlers) handleGraceDestroy(ctx context.Context, job *models.Job) error {
	serviceTag := h.payloadString(job, "serviceTag")
	pool, err := h.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	if pool == nil {
		return nil
	}
	if pool.RefCount > 0 {
		log.Printf("grace_destroy cancelled: refCount=%d for %s", pool.RefCount, serviceTag)
		return nil
	}
	if pool.State != models.GPUPoolStateDraining {
		log.Printf("grace_destroy skipped: state=%s for %s", pool.State, serviceTag)
		return nil
	}
	if !pool.HasScheduledGraceDestroy() {
		log.Printf("grace_destroy skipped: drainReason=%s for %s", pool.DrainReason, serviceTag)
		return nil
	}
	return h.handleDestroy(ctx, job)
}

func (h *GPUPoolHandlers) handleHealthCheck(ctx context.Context, job *models.Job) error {
	jl, err := h.openJobLog(job)
	if err != nil {
		log.Printf("job log unavailable: %v", err)
	} else {
		defer jl.close()
	}

	serviceName := h.payloadString(job, "serviceName")
	if serviceName == "" {
		serviceName = "sign_verify_vlm_gpu"
	}
	return h.runScript(ctx, jl, job, "scripts/e2e_ops.sh", "deploy", serviceName, "--api")
}

func (h *GPUPoolHandlers) handleMeterTick(ctx context.Context, job *models.Job) error {
	serviceTag := h.payloadString(job, "serviceTag")
	userID := h.payloadString(job, "userId")
	if serviceTag == "" || userID == "" {
		return fmt.Errorf("meter tick job missing serviceTag/userId")
	}
	return h.gpuPoolService.HandleMeterTick(ctx, serviceTag, userID)
}
