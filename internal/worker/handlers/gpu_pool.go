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
	betaServiceRepo        repository.BetaServiceRepository
	gpuPoolService         services.GPUPoolService
	provisionConfigService services.GPUProvisionConfigService
}

func NewGPUPoolHandlers(cfg *config.Config, poolRepo repository.GPUPoolRepository, jobRepo repository.JobRepository, betaServiceRepo repository.BetaServiceRepository, gpuPoolService services.GPUPoolService, provisionConfigService services.GPUProvisionConfigService) *GPUPoolHandlers {
	return &GPUPoolHandlers{
		cfg:                    cfg,
		poolRepo:               poolRepo,
		jobRepo:                jobRepo,
		betaServiceRepo:        betaServiceRepo,
		gpuPoolService:         gpuPoolService,
		provisionConfigService: provisionConfigService,
	}
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

func gpuPoolBootstrapStages(serviceTag string) string {
	switch serviceTag {
	case models.OCRGPUServiceTag:
		return "e2e-nvidia,e2e-sync,e2e-deploy,e2e-smoke"
	default:
		return "e2e-nvidia,e2e-sync,e2e-ensure-ollama,e2e-ollama,e2e-deploy,e2e-smoke"
	}
}

func gpuPoolPostBootstrapStages(serviceTag string) string {
	switch serviceTag {
	case models.OCRGPUServiceTag:
		return "gateway,gateway-smoke,gpu-pool-sync"
	default:
		return "gateway,gateway-smoke,beta-seed,gpu-pool-sync"
	}
}

func registryAuthModeForService(service *models.BetaService) string {
	if service == nil || service.RegistrySettings == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(service.RegistrySettings.Provider)) {
	case "ecr":
		return "ecr"
	}
	switch strings.ToLower(strings.TrimSpace(service.RegistrySettings.Auth)) {
	case "aws":
		return "ecr"
	case "none", "":
		return ""
	default:
		return "static"
	}
}

func registryBoolEnv(name string, value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return name + "=1"
	}
	return name + "=0"
}

func (h *GPUPoolHandlers) workerRegistryEnv(serviceTag, serviceName string) []string {
	if h.betaServiceRepo == nil || serviceTag == "" {
		return nil
	}
	service, err := h.betaServiceRepo.GetByTag(context.Background(), serviceTag)
	if err != nil || service == nil || service.RegistrySettings == nil || service.RegistrySettings.IsEmpty() {
		return nil
	}
	reg := service.RegistrySettings
	lines := []string{}
	if provider := strings.TrimSpace(reg.Provider); provider != "" {
		lines = append(lines, "REGISTRY_PROVIDER="+provider)
	}
	if authMode := registryAuthModeForService(service); authMode != "" {
		lines = append(lines, "REGISTRY_AUTH_MODE="+authMode)
	}
	if reg.Server != "" {
		lines = append(lines, "REGISTRY_SERVER="+reg.Server)
	}
	if reg.Region != "" {
		// Registry region is for ECR login/pull only. Never set AWS_REGION here —
		// that overrides GPU provision (EC2 subnet/SG live in ap-south-1) and
		// produces InvalidSubnetID.NotFound when ECR defaults to eu-north-1.
		lines = append(lines, "REGISTRY_ECR_REGION="+reg.Region)
	}
	if reg.Server != "" && reg.Namespace != "" {
		fullNamespace := reg.Server + "/" + reg.Namespace
		lines = append(lines, "REGISTRY_NAMESPACE_FULL="+fullNamespace)
		if serviceName == "ocr" {
			lines = append(lines, "OCR_REGISTRY_NAMESPACE="+fullNamespace)
		}
		if reg.ImageTag != "" {
			lines = append(lines, "DOCKER_PULL_IMAGE="+fullNamespace+"/"+serviceName+":"+reg.ImageTag)
		}
	}
	if reg.ImageTag != "" {
		lines = append(lines, "REGISTRY_IMAGE_TAG="+reg.ImageTag)
		if serviceName == "ocr" {
			lines = append(lines, "OCR_IMAGE_TAG="+reg.ImageTag)
		}
	}
	if line := registryBoolEnv("PREFER_REGISTRY_PULL", reg.PreferRegistryPull); line != "" {
		lines = append(lines, line)
	}
	if line := registryBoolEnv("REGISTRY_LOGIN_REQUIRED", reg.LoginRequired); line != "" {
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		lines = append(lines, "BETA_SERVICE_REGISTRY_SOURCE=beta-services")
	}
	return lines
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
	}
	if job != nil {
		env = append(env, fmt.Sprintf("WORKER_JOB_ATTEMPT=%d", job.Attempts))
		serviceTag := h.payloadString(job, "serviceTag")
		serviceName := h.payloadString(job, "serviceName")
		if serviceTag != "" {
			env = append(env, "PIPELINE_STAGE="+gpuPoolBootstrapStages(serviceTag))
			env = append(env, "NVIDIA_MIN_DRIVER_MAJOR="+nvidiaMinDriverMajor(serviceTag))
		}
		if serviceTag != "" && h.provisionConfigService != nil {
			if cfg, err := h.provisionConfigService.GetOrDefault(context.Background(), serviceTag); err == nil && cfg != nil {
				env = append(env, h.provisionConfigService.WorkerEnv(cfg)...)
				if serviceName == "" {
					serviceName = cfg.ServiceName
				}
			}
		}
		env = append(env, h.workerRegistryEnv(serviceTag, serviceName)...)
	}
	return env
}

func nvidiaMinDriverMajor(serviceTag string) string {
	switch models.CanonicalGPUServiceTag(serviceTag) {
	case models.OCRGPUServiceTag:
		return "580"
	default:
		return "550"
	}
}

type provisionState struct {
	Provider         string `json:"provider"`
	NodeID           string `json:"node_id"`
	PublicIP         string `json:"public_ip"`
	PreviousPublicIP string `json:"previous_public_ip"`
	Status           string `json:"status"`
}

func inferProvider(nodeID, provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if strings.HasPrefix(strings.TrimSpace(nodeID), "i-") {
		return "aws"
	}
	if provider != "" {
		return provider
	}
	return "e2e"
}

func (h *GPUPoolHandlers) provisionStatePaths() []string {
	root := h.automicaRoot()
	reg := filepath.Join(root, "services/pipeline/registry")
	paths := []string{
		filepath.Join(reg, ".gpu_provision_state.json"),
		filepath.Join(reg, ".e2e_provision_state.json"),
	}
	if matches, err := filepath.Glob(filepath.Join(reg, ".gpu_provision_state*.json")); err == nil {
		for _, m := range matches {
			paths = append(paths, m)
		}
	}
	return paths
}

// providerNodeStillLive returns true when destroy claimed success but the VM is still present.
func (h *GPUPoolHandlers) providerNodeStillLive(ctx context.Context, serviceTag, provider, nodeID, publicIP string) bool {
	root := h.automicaRoot()
	provider = inferProvider(nodeID, provider)
	env := append(os.Environ(),
		"AUTOMICA_ROOT="+root,
		"GPU_PROVIDER="+provider,
		"GPU_SERVICE_TAG="+serviceTag,
	)
	if h.provisionConfigService != nil && serviceTag != "" {
		if cfg, err := h.provisionConfigService.GetOrDefault(ctx, serviceTag); err == nil && cfg != nil {
			env = append(env, h.provisionConfigService.WorkerEnv(cfg)...)
			env = append(env, "GPU_PROVIDER="+provider)
		}
	}
	if nodeID != "" {
		cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts/gpu_provision_node.sh"), "node-live")
		cmd.Dir = root
		cmd.Env = env
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts/gpu_provision_node.sh"), "list-nodes")
	cmd.Dir = root
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("destroy verify list-nodes failed provider=%s: %v", provider, err)
		return true
	}
	text := string(out)
	if nodeID != "" && strings.Contains(text, "id="+nodeID) {
		return true
	}
	if publicIP != "" && publicIP != "-" && strings.Contains(text, publicIP) {
		return true
	}
	return false
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
	provider := inferProvider(st.NodeID, st.Provider)
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

// Prefer reuse whenever the tracked state still looks alive, even if the provider
// list is briefly stale. That avoids tearing down a live node on a false negative.
func shouldReuseProvisionNode(st *provisionState, nodeLive bool) bool {
	return provisionStateReusable(st) || nodeLive
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
		if err == nil && shouldReuseProvisionNode(st, h.nodeListed(st)) {
			log.Printf("provision attempt=%d/%d: reusing tracked GPU node ip=%s status=%s", job.Attempts, job.MaxAttempts, st.PublicIP, st.Status)
			return append(args, "--reuse-node")
		}
		if job.Attempts > 1 {
			h.clearProvisionState()
			if err == nil {
				log.Printf("provision retry attempt=%d/%d: tracked node not listed; fresh create", job.Attempts, job.MaxAttempts)
			} else {
				log.Printf("provision retry attempt=%d/%d: no reusable state; fresh create", job.Attempts, job.MaxAttempts)
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
	env := append(os.Environ(), append(h.workerBootstrapEnv(root, job),
		"PIPELINE_STAGE="+stages,
	)...)
	// Capture IP before gateway stages: shared state files can disappear under reconcile.
	if st, err := h.readProvisionState(); err == nil && strings.TrimSpace(st.PublicIP) != "" {
		ip := strings.TrimSpace(st.PublicIP)
		env = append(env, "E2E_HOST_IP="+ip, "GPU_PUBLIC_IP="+ip)
	}
	cmd.Env = env

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

func (h *GPUPoolHandlers) servicePortOnNode(ctx context.Context, serviceName string) string {
	root := h.automicaRoot()
	script := `set -euo pipefail
root="$1"
service="$2"
source "$root/services/pipeline/pipeline_common.sh"
load_service_config "$service"
printf '%s' "${E2E_PORT:-}"
`
	cmd := exec.CommandContext(ctx, "bash", "-s", "--", root, serviceName)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+root)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (h *GPUPoolHandlers) serviceHealthyOnNode(ctx context.Context, serviceName string) bool {
	if serviceName == "" {
		return false
	}
	port := h.servicePortOnNode(ctx, serviceName)
	if port == "" {
		return false
	}
	// Keep this deliberately small: it is a retry shortcut, not the acceptance smoke.
	// /health is enough here because the pipeline already runs the real OCR smoke path
	// before marking the pool ready.
	sshHost := "e2e"
	if st, err := h.readProvisionState(); err == nil && st != nil {
		if inferProvider(st.NodeID, st.Provider) == "aws" {
			sshHost = "vlm-aws"
		}
	}
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", sshHost,
		fmt.Sprintf("curl -sf --connect-timeout 5 --max-time 10 http://127.0.0.1:%s/health >/dev/null", port))
	cmd.Dir = h.automicaRoot()
	cmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+h.automicaRoot())
	return cmd.Run() == nil
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
	updates := map[string]any{
		"state":        models.GPUPoolStateFailed,
		"lastError":    services.SanitizeGPUPoolUserError(errMsg, hintMin),
		"lastErrorRaw": errMsg,
	}
	if st, err := h.readProvisionState(); err == nil {
		nodeLive := h.nodeListed(st)
		if shouldReuseProvisionNode(st, nodeLive) {
			if st.NodeID != "" {
				updates["nodeId"] = st.NodeID
			}
			if st.PublicIP != "" {
				updates["publicIp"] = st.PublicIP
			}
			if st.PreviousPublicIP != "" {
				updates["previousPublicIp"] = st.PreviousPublicIP
			}
			if provider := strings.TrimSpace(st.Provider); provider != "" {
				updates["provider"] = provider
			}
			log.Printf("provision failed for %s but node is still SSH-reusable; preserving node metadata for grace reuse", serviceTag)
		}
	}
	_, err := h.poolRepo.Update(ctx, serviceTag, updates)
	if err != nil {
		log.Printf("mark provision failed for %s: %v", serviceTag, err)
		return
	}
	if err := h.gpuPoolService.HandleProvisionFailed(ctx, serviceTag); err != nil {
		log.Printf("refund failed pool sessions for %s: %v", serviceTag, err)
	}
}

func (h *GPUPoolHandlers) failProvisionIfFinal(ctx context.Context, job *models.Job, pool *models.GPUPool, serviceTag string, err error) error {
	if err == nil {
		return nil
	}
	terminal := services.IsTerminalProvisionError(err.Error())
	if terminal && pool != nil && pool.RefCount == 0 {
		if abortErr := h.gpuPoolService.AdminAbortProvision(ctx, serviceTag); abortErr != nil {
			log.Printf("terminal provision abort failed for %s: %v", serviceTag, abortErr)
		}
	}
	if job.Attempts >= job.MaxAttempts || terminal {
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
		return h.failProvisionIfFinal(ctx, job, pool, serviceTag, err)
	}
	if pool == nil {
		err = fmt.Errorf("gpu pool not found: %s", serviceTag)
		return h.failProvisionIfFinal(ctx, job, nil, serviceTag, err)
	}
	if pool.RefCount == 0 {
		log.Printf("provision skipped: refCount=0 for %s", serviceTag)
		if err := h.gpuPoolService.OnProvisionSkippedNoSessions(ctx, serviceTag); err != nil {
			log.Printf("provision skip handling for %s: %v", serviceTag, err)
		}
		return nil
	}
	if pool.State == models.GPUPoolStateDraining {
		log.Printf("provision skipped: pool is draining for %s", serviceTag)
		return nil
	}

	// Full bootstrap on the GPU node (provider from policy: E2E Networks and/or AWS).
	skipBootstrap := false
	healthCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	skipBootstrap = h.serviceHealthyOnNode(healthCtx, serviceName)
	cancel()
	if skipBootstrap {
		log.Printf("provision attempt=%d/%d: service already healthy; resuming post-bootstrap stages for %s", job.Attempts, job.MaxAttempts, serviceTag)
		if jl != nil {
			jl.writeLine("worker", fmt.Sprintf("resume: service already healthy; skipping bootstrap for %s", serviceTag))
		}
	}
	if !skipBootstrap {
		bootstrapArgs := h.bootstrapArgsForJob(ctx, job, serviceTag, serviceName)
		if err := h.runScript(ctx, jl, job, "scripts/gpu_bootstrap.sh", bootstrapArgs...); err != nil {
			if worker.IsJobCancelled(err) {
				return nil
			}
			return h.failProvisionIfFinal(ctx, job, pool, serviceTag, err)
		}
	}

	pool, err = h.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return h.failProvisionIfFinal(ctx, job, pool, serviceTag, err)
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
	if err := h.runPipeline(ctx, jl, job, serviceName, gpuPoolPostBootstrapStages(serviceTag)); err != nil {
		if worker.IsJobCancelled(err) {
			return nil
		}
		return h.failProvisionIfFinal(ctx, job, pool, serviceTag, err)
	}

	pool, err = h.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return h.failProvisionIfFinal(ctx, job, pool, serviceTag, err)
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
		// OCR (or any pool) must not terminate a VM still serving another pool — that caused
		// repeated gateway 502s when orphan-recover stole Sign Verify's AWS nodeId.
		if pool != nil && h.gpuPoolService != nil {
			if claimed, claimErr := h.gpuPoolService.NodeClaimedByOtherPool(ctx, serviceTag, pool.NodeID, pool.PublicIP); claimErr != nil {
				return claimErr
			} else if claimed != nil {
				log.Printf("destroy detach-only: nodeId=%s ip=%s still owned by %s (ref=%d state=%s); clearing %s claim",
					pool.NodeID, pool.PublicIP, claimed.ServiceTag, claimed.RefCount, claimed.State, serviceTag)
				_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
					"state":          models.GPUPoolStateIdle,
					"refCount":       0,
					"nodeId":         "",
					"publicIp":       "",
					"drainStartedAt": nil,
					"drainReason":    "",
					"destroyAt":      nil,
					"readyAt":        nil,
					"lastError":      "",
					"lastErrorRaw":   "",
				})
				return err
			}
		}
		switch {
		case pool != nil && pool.NodeID != "":
			// Mac bootstrap writes nodeId to Mongo via gpu-pool-sync; dev2 has no local state file.
			provider := inferProvider(pool.NodeID, pool.Provider)
			log.Printf("destroy: provider=%s Mongo nodeId=%s serviceTag=%s", provider, pool.NodeID, serviceTag)
			extraEnv := []string{}
			extraEnv = append(extraEnv, "GPU_PROVIDER="+provider)
			destroyErr = h.runScriptWithExtraEnv(ctx, jl, job, extraEnv, "scripts/gpu_provision_node.sh", "destroy", pool.NodeID)
		case pool != nil && pool.PublicIP != "":
			provider := inferProvider(pool.NodeID, pool.Provider)
			log.Printf("destroy: provider=%s resolve by publicIp=%s serviceTag=%s", provider, pool.PublicIP, serviceTag)
			extraEnv := []string{"E2E_DESTROY_PUBLIC_IP=" + pool.PublicIP}
			extraEnv = append(extraEnv, "GPU_PROVIDER="+provider)
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
		log.Printf("destroy script failed; preserving pool state for retry: %v", destroyErr)
		if serviceTag != "" {
			_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
				"state":        models.GPUPoolStateDraining,
				"lastError":    services.SanitizeGPUPoolUserError(destroyErr.Error(), 15),
				"lastErrorRaw": destroyErr.Error(),
			})
			if err != nil {
				return err
			}
		}
		return destroyErr
	}
	// Honest idle: only clear Mongo when the provider confirms the node is gone.
	if serviceTag != "" {
		poolAfter, _ := h.poolRepo.GetByServiceTag(ctx, serviceTag)
		if poolAfter != nil && (strings.TrimSpace(poolAfter.NodeID) != "" || strings.TrimSpace(poolAfter.PublicIP) != "") {
			provider := inferProvider(poolAfter.NodeID, poolAfter.Provider)
			if h.providerNodeStillLive(ctx, serviceTag, provider, poolAfter.NodeID, poolAfter.PublicIP) {
				msg := fmt.Sprintf("destroy reported success but provider still has nodeId=%s ip=%s", poolAfter.NodeID, poolAfter.PublicIP)
				log.Printf("%s", msg)
				_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
					"state":        models.GPUPoolStateDraining,
					"lastError":    services.SanitizeGPUPoolUserError(msg, 15),
					"lastErrorRaw": msg,
				})
				if err != nil {
					return err
				}
				return fmt.Errorf("%s", msg)
			}
		}
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
	if h.gpuPoolService != nil {
		if claimed, claimErr := h.gpuPoolService.NodeClaimedByOtherPool(ctx, serviceTag, pool.NodeID, pool.PublicIP); claimErr != nil {
			return claimErr
		} else if claimed != nil {
			log.Printf("grace_destroy detach-only: node still owned by %s; clearing %s claim", claimed.ServiceTag, serviceTag)
			_, err := h.poolRepo.Update(ctx, serviceTag, map[string]any{
				"state":          models.GPUPoolStateIdle,
				"refCount":       0,
				"nodeId":         "",
				"publicIp":       "",
				"drainStartedAt": nil,
				"drainReason":    "",
				"destroyAt":      nil,
				"readyAt":        nil,
				"lastError":      "",
				"lastErrorRaw":   "",
			})
			return err
		}
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
