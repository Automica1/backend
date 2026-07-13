package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	cfg                    *config.Config
	poolRepo               repository.GPUPoolRepository
	jobRepo                repository.JobRepository
	gpuPoolService         services.GPUPoolService
	provisionConfigService services.GPUProvisionConfigService
}

func NewGPUPoolHandlers(cfg *config.Config, poolRepo repository.GPUPoolRepository, jobRepo repository.JobRepository, gpuPoolService services.GPUPoolService, provisionConfigService services.GPUProvisionConfigService) *GPUPoolHandlers {
	return &GPUPoolHandlers{cfg: cfg, poolRepo: poolRepo, jobRepo: jobRepo, gpuPoolService: gpuPoolService, provisionConfigService: provisionConfigService}
}

func (h *GPUPoolHandlers) Register(registry *worker.Registry) {
	registry.Register(models.JobTypeGPUPoolProvision, h.handleProvision)
	registry.Register(models.JobTypeGPUPoolDestroy, h.handleDestroy)
	registry.Register(models.JobTypeGPUPoolGraceDestroy, h.handleGraceDestroy)
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

// workerBootstrapEnv builds shell env for GPU pool worker jobs from policy (no hardcoded timeouts).
func (h *GPUPoolHandlers) workerBootstrapEnv(root string, job *models.Job) []string {
	target := os.Getenv("AUTOMICA_TARGET")
	if target == "" {
		target = "dev2"
	}
	env := []string{
		"AUTOMICA_ROOT=" + root,
		"AUTOMICA_TARGET=" + target,
		"ROOT=" + root,
		"SKIP_LOCAL_PREP=1",
		"SKIP_OLLAMA_OFFLINE_PREP=1",
		"E2E_USE_SAVED_IMAGE=0",
		"GPU_POOL_SYNC_NODE_OWNER=user",
		"PIPELINE_STAGE=e2e-nvidia,e2e-sync,e2e-ensure-ollama,e2e-ollama,e2e-deploy,e2e-smoke",
	}
	if job != nil {
		env = append(env, fmt.Sprintf("WORKER_JOB_ATTEMPT=%d", job.Attempts))
		if tag := h.payloadString(job, "serviceTag"); tag != "" && h.provisionConfigService != nil {
			if cfg, err := h.provisionConfigService.GetOrDefault(context.Background(), tag); err == nil && cfg != nil {
				env = append(env, h.provisionConfigService.WorkerEnv(cfg)...)
			}
		}
	}
	return env
}

type provisionState struct {
	Provider         string `json:"provider"`
	NodeID           string `json:"node_id"`
	PublicIP         string `json:"public_ip"`
	PreviousPublicIP string `json:"previous_public_ip"`
	Status           string `json:"status"`
}

func (h *GPUPoolHandlers) provisionStatePaths() []string {
	root := h.automicaRoot()
	reg := filepath.Join(root, "services/pipeline/registry")
	return []string{
		filepath.Join(reg, ".gpu_provision_state.json"),
		filepath.Join(reg, ".e2e_provision_state.json"),
	}
}

func (h *GPUPoolHandlers) provisionStatePath() string {
	return h.provisionStatePaths()[0]
}

func (h *GPUPoolHandlers) clearProvisionState() {
	for _, p := range h.provisionStatePaths() {
		_ = os.Remove(p)
	}
}

func (h *GPUPoolHandlers) nodeListed(st *provisionState) bool {
	if st == nil || st.NodeID == "" {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(st.Provider))
	if provider == "" {
		provider = "e2e"
	}
	root := h.automicaRoot()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts/gpu_provision_node.sh"), "node-live")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+root, "GPU_PROVIDER="+provider)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
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

func (h *GPUPoolHandlers) bootstrapArgsForJob(ctx context.Context, job *models.Job, serviceTag, serviceName string) []string {
	args := []string{serviceName}
	reuseAllowed := true
	if h.provisionConfigService != nil && serviceTag != "" {
		if cfg, err := h.provisionConfigService.GetOrDefault(ctx, serviceTag); err == nil && cfg != nil {
			reuseAllowed = cfg.Retries.ReuseNodeOnRetry
		}
	}
	if reuseAllowed {
		st, err := h.readProvisionState()
		if err == nil && provisionStateReusable(st) {
			if h.nodeListed(st) {
				log.Printf("provision attempt=%d/%d: reusing live node ip=%s status=%s", job.Attempts, job.MaxAttempts, st.PublicIP, st.Status)
				return append(args, "--reuse-node")
			}
			if job.Attempts > 1 {
				h.clearProvisionState()
				log.Printf("provision retry attempt=%d/%d: fresh create (state not live)", job.Attempts, job.MaxAttempts)
			}
		}
	} else if job.Attempts > 1 {
		h.clearProvisionState()
		log.Printf("provision retry attempt=%d/%d: fresh create (reuse disabled)", job.Attempts, job.MaxAttempts)
	}
	return append(args, "--fresh")
}

func (h *GPUPoolHandlers) runScript(ctx context.Context, jl *jobLog, job *models.Job, script string, args ...string) error {
	return h.runScriptWithExtraEnv(ctx, jl, job, nil, script, args...)
}

func (h *GPUPoolHandlers) startCancelWatcher(ctx context.Context, job *models.Job, cmdPtr *atomic.Pointer[exec.Cmd]) (context.Context, context.CancelFunc) {
	watchCtx, cancel := context.WithCancel(ctx)
	if h.jobRepo == nil || job == nil {
		return watchCtx, cancel
	}
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				j, err := h.jobRepo.GetByID(context.Background(), job.ID)
				if err != nil || j == nil {
					continue
				}
				if j.Status != models.JobStatusCancelled {
					continue
				}
				if c := cmdPtr.Load(); c != nil && c.Process != nil {
					_ = c.Process.Kill()
				}
				cancel()
				return
			}
		}
	}()
	return watchCtx, cancel
}

func (h *GPUPoolHandlers) finishCmdRun(ctx context.Context, watchCtx context.Context, job *models.Job, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(watchCtx.Err(), context.Canceled) {
		if h.jobRepo != nil && job != nil {
			j, getErr := h.jobRepo.GetByID(ctx, job.ID)
			if getErr == nil && j != nil && j.Status == models.JobStatusCancelled {
				return worker.ErrJobCancelled
			}
		}
	}
	return err
}

func (h *GPUPoolHandlers) runScriptWithExtraEnv(ctx context.Context, jl *jobLog, job *models.Job, extraEnv []string, script string, args ...string) error {
	root := h.automicaRoot()
	cmdPath := filepath.Join(root, script)
	var cmdPtr atomic.Pointer[exec.Cmd]
	watchCtx, stopWatch := h.startCancelWatcher(ctx, job, &cmdPtr)
	defer stopWatch()

	cmd := exec.CommandContext(watchCtx, "bash", append([]string{cmdPath}, args...)...)
	cmdPtr.Store(cmd)
	cmd.Dir = root
	baseEnv := h.workerBootstrapEnv(root, job)
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

	err := h.finishCmdRun(ctx, watchCtx, job, cmd.Run())
	if stdout.Len() > 0 {
		log.Printf("stdout:\n%s", stdout.String())
	}
	if stderr.Len() > 0 {
		log.Printf("stderr:\n%s", stderr.String())
	}
	if err != nil {
		if worker.IsJobCancelled(err) {
			return err
		}
		return fmt.Errorf("%s: %w\nstderr: %s", script, err, stderr.String())
	}
	return nil
}

func (h *GPUPoolHandlers) runPipeline(ctx context.Context, jl *jobLog, job *models.Job, serviceName, stages string) error {
	root := h.automicaRoot()
	pipeline := filepath.Join(root, "services/pipeline/run_pipeline.sh")
	var cmdPtr atomic.Pointer[exec.Cmd]
	watchCtx, stopWatch := h.startCancelWatcher(ctx, job, &cmdPtr)
	defer stopWatch()

	cmd := exec.CommandContext(watchCtx, "bash", pipeline, serviceName)
	cmdPtr.Store(cmd)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), append(h.workerBootstrapEnv(root, job),
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

	err := h.finishCmdRun(ctx, watchCtx, job, cmd.Run())
	if out.Len() > 0 {
		log.Printf("pipeline stdout:\n%s", out.String())
	}
	if err != nil {
		if worker.IsJobCancelled(err) {
			return err
		}
		return fmt.Errorf("pipeline %s: %w\n%s", stages, err, out.String())
	}
	return nil
}

func (h *GPUPoolHandlers) readProvisionState() (*provisionState, error) {
	var data []byte
	var err error
	for _, statePath := range h.provisionStatePaths() {
		data, err = os.ReadFile(statePath)
		if err == nil {
			break
		}
	}
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
	hintMin := 15
	if h.provisionConfigService != nil {
		if cfg, err := h.provisionConfigService.GetOrDefault(ctx, serviceTag); err == nil && cfg != nil {
			cfg.Normalize()
			if cfg.Lifecycle.UserRetryHintMin > 0 {
				hintMin = cfg.Lifecycle.UserRetryHintMin
			}
		}
	}
	_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":        models.GPUPoolStateFailed,
		"lastError":    services.SanitizeGPUPoolUserError(errMsg, hintMin),
		"lastErrorRaw": errMsg,
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

	// Full bootstrap on the GPU node (provider from policy: E2E Networks and/or AWS).
	bootstrapArgs := h.bootstrapArgsForJob(ctx, job, serviceTag, serviceName)
	if err := h.runScript(ctx, jl, job, "scripts/gpu_bootstrap.sh", bootstrapArgs...); err != nil {
		if worker.IsJobCancelled(err) {
			return nil
		}
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}

	pool, err = h.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}
	if pool != nil && pool.RefCount == 0 {
		cont, handleErr := h.gpuPoolService.HandleProvisionNoSessions(ctx, serviceTag, false)
		if handleErr != nil {
			log.Printf("provision post-bootstrap no-session for %s: %v", serviceTag, handleErr)
		}
		if !cont {
			return nil
		}
	}

	// Gateway + beta registry + Mongo pool ready (same stages as Mac bootstrap pipeline).
	if err := h.runPipeline(ctx, jl, job, serviceName, "gateway,gateway-smoke,beta-seed,gpu-pool-sync"); err != nil {
		if worker.IsJobCancelled(err) {
			return nil
		}
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}

	pool, err = h.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return h.failProvisionIfFinal(ctx, job, serviceTag, err)
	}
	if pool != nil && pool.RefCount == 0 {
		if _, handleErr := h.gpuPoolService.HandleProvisionNoSessions(ctx, serviceTag, true); handleErr != nil {
			log.Printf("provision post-pipeline no-session for %s: %v", serviceTag, handleErr)
		}
		return nil
	}

	if err := h.runScript(ctx, jl, job, "scripts/aws_ensure_gpu_firewall.sh", serviceName); err != nil {
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
		if pool != nil && pool.RefCount > 0 {
			log.Printf("destroy cancelled: refCount=%d for %s", pool.RefCount, serviceTag)
			return nil
		}
		switch {
		case pool != nil && pool.NodeID != "":
			// Mac bootstrap writes nodeId to Mongo via gpu-pool-sync; dev2 has no local state file.
			log.Printf("destroy: Mongo nodeId=%s serviceTag=%s", pool.NodeID, serviceTag)
			extraEnv := []string{}
			if pool.Provider != "" {
				extraEnv = append(extraEnv, "GPU_PROVIDER="+pool.Provider)
			}
			destroyErr = h.runScriptWithExtraEnv(ctx, jl, job, extraEnv, "scripts/gpu_provision_node.sh", "destroy", pool.NodeID)
		case pool != nil && pool.PublicIP != "":
			log.Printf("destroy: resolve by publicIp=%s serviceTag=%s", pool.PublicIP, serviceTag)
			extraEnv := []string{"E2E_DESTROY_PUBLIC_IP=" + pool.PublicIP}
			if pool.Provider != "" {
				extraEnv = append(extraEnv, "GPU_PROVIDER="+pool.Provider)
			}
			destroyErr = h.runScriptWithExtraEnv(ctx, jl, job,
				extraEnv,
				"scripts/gpu_provision_node.sh", "destroy")
		default:
			destroyErr = h.runScript(ctx, jl, job, "scripts/gpu_bootstrap.sh", "--destroy")
		}
	} else {
		destroyErr = h.runScript(ctx, jl, job, "scripts/gpu_bootstrap.sh", "--destroy")
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
			"lastErrorRaw":   "",
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

func (h *GPUPoolHandlers) handleMeterTick(ctx context.Context, job *models.Job) error {
	serviceTag := h.payloadString(job, "serviceTag")
	userID := h.payloadString(job, "userId")
	if serviceTag == "" || userID == "" {
		return fmt.Errorf("meter tick job missing serviceTag/userId")
	}
	return h.gpuPoolService.HandleMeterTick(ctx, serviceTag, userID)
}
