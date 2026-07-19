package handlers

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func gpuProvisionKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:provision", serviceTag)
}

func (h *GPUPoolHandler) automicaRoot() string {
	candidates := []string{}
	if h.cfg != nil && h.cfg.Worker.AutomicaRoot != "" {
		candidates = append(candidates, h.cfg.Worker.AutomicaRoot)
	}
	if v := os.Getenv("AUTOMICA_ROOT"); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, "/home/ec2-user/automica", ".")
	for _, root := range candidates {
		if root == "" || root == "." {
			continue
		}
		if st, err := os.Stat(filepath.Join(root, "scripts", "gpu_provision_node.sh")); err == nil && !st.IsDir() {
			return root
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "."
}

func (h *GPUPoolHandler) workerLogCandidates(job *models.Job) []string {
	roots := []string{h.automicaRoot(), "/home/ec2-user/automica"}
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	for _, root := range roots {
		dir := filepath.Join(root, "logs", "worker")
		add(filepath.Join(dir, "latest.log"))
		if job != nil && !job.ID.IsZero() {
			add(filepath.Join(dir, fmt.Sprintf("%s-%s.log", job.Type, job.ID.Hex())))
		}
	}
	return paths
}

func (h *GPUPoolHandler) enrichAdminPool(ctx context.Context, pool *models.GPUPool) models.GPUPoolAdminEntry {
	if pool == nil {
		return models.GPUPoolAdminEntry{}
	}
	entry := models.GPUPoolAdminEntry{GPUPool: *pool}
	tag := pool.ServiceTag
	if cfg, err := h.provisionConfigService.GetOrDefault(ctx, tag); err == nil && cfg != nil {
		entry.GracePeriodSec = cfg.Lifecycle.GracePeriodMin * 60
		if entry.GracePeriodSec <= 0 {
			entry.GracePeriodSec = 300
		}
		entry.PolicySummary = h.provisionConfigService.EffectiveSummary(cfg)
	}
	if job, _ := h.jobRepo.GetByIdempotencyKey(ctx, gpuProvisionKey(tag)); job != nil {
		if job.Status == models.JobStatusPending || job.Status == models.JobStatusRunning {
			entry.ActiveJob = job
		}
	}
	return entry
}

func (h *GPUPoolHandler) GetAdminPool(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	pool, err := h.gpuPoolService.ListAdmin(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	for _, p := range pool {
		if p != nil && p.ServiceTag == tag {
			entry := h.enrichAdminPool(r.Context(), p)
			cfg, cfgErr := h.provisionConfigService.GetOrDefault(r.Context(), tag)
			resp := map[string]any{
				"pool":          entry,
				"policy":        cfg,
				"policySummary": entry.PolicySummary,
				"billing":       h.billingInfo(),
			}
			if cfgErr != nil {
				utils.SendErrorResponse(w, cfgErr)
				return
			}
			utils.SendJSONResponse(w, http.StatusOK, resp)
			return
		}
	}
	utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, "gpu pool not found"))
}

func (h *GPUPoolHandler) billingInfo() models.GPUPoolBillingInfo {
	startup, perMin := 20, 2
	if h.cfg != nil {
		if h.cfg.Worker.StartupCredits > 0 {
			startup = h.cfg.Worker.StartupCredits
		}
		if h.cfg.Worker.CreditsPerMin > 0 {
			perMin = h.cfg.Worker.CreditsPerMin
		}
	}
	return models.GPUPoolBillingInfo{
		StartupCredits: startup,
		CreditsPerMin:  perMin,
		ReadOnly:       true,
	}
}

func (h *GPUPoolHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	cfg, err := h.provisionConfigService.GetOrDefault(r.Context(), tag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{
		"policy":        cfg,
		"policySummary": h.provisionConfigService.EffectiveSummary(cfg),
	})
}

func (h *GPUPoolHandler) PutPolicy(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	var cfg models.GPUProvisionConfig
	if err := utils.DecodeJSONBody(r, &cfg); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	cfg.ServiceTag = tag
	if email, ok := middleware.GetEmailFromContext(r.Context()); ok {
		cfg.UpdatedBy = email
	}
	saved, err := h.provisionConfigService.Upsert(r.Context(), &cfg)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{
		"policy":        saved,
		"policySummary": h.provisionConfigService.EffectiveSummary(saved),
	})
}

func (h *GPUPoolHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	jobs, err := h.jobRepo.ListGPUByServiceTag(r.Context(), tag, 30)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *GPUPoolHandler) GetJobLog(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	jobID := r.URL.Query().Get("jobId")

	var job *models.Job
	var err error
	if jobID != "" {
		oid, parseErr := primitive.ObjectIDFromHex(jobID)
		if parseErr != nil {
			utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrValidation, http.StatusBadRequest, "invalid jobId"))
			return
		}
		job, err = h.jobRepo.GetByID(r.Context(), oid)
	} else {
		job, err = h.jobRepo.GetByIdempotencyKey(r.Context(), gpuProvisionKey(tag))
	}
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	lines, path, readErr := h.tailJobLog(job, 500)
	if readErr != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, readErr.Error()))
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{
		"path":    path,
		"lines":   lines,
		"jobId":   jobIDFromJob(job),
		"jobType": jobTypeFromJob(job),
	})
}

func jobIDFromJob(job *models.Job) string {
	if job == nil {
		return ""
	}
	return job.ID.Hex()
}

func jobTypeFromJob(job *models.Job) string {
	if job == nil {
		return ""
	}
	return job.Type
}

func (h *GPUPoolHandler) tailJobLog(job *models.Job, maxLines int) ([]string, string, error) {
	for _, path := range h.workerLogCandidates(job) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
		return lines, path, nil
	}
	return nil, "", fmt.Errorf("log file not found")
}

func (h *GPUPoolHandler) RunDiagnostics(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	pools, err := h.gpuPoolService.ListAdmin(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	var pool *models.GPUPool
	for _, p := range pools {
		if p != nil && p.ServiceTag == tag {
			pool = p
			break
		}
	}
	if pool == nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, "gpu pool not found"))
		return
	}

	result := h.probePool(r.Context(), pool)
	utils.SendJSONResponse(w, http.StatusOK, result)
}

func (h *GPUPoolHandler) probePool(ctx context.Context, pool *models.GPUPool) map[string]any {
	root := h.automicaRoot()
	cfg, _ := h.provisionConfigService.GetOrDefault(ctx, pool.ServiceTag)
	provider := strings.TrimSpace(pool.Provider)
	if provider == "" && cfg != nil {
		provider = string(cfg.Infrastructure.PrimaryProvider)
	}
	if provider == "" {
		provider = "e2e"
	}
	summary := []string{}
	nodeLive := false
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts/gpu_provision_node.sh"), "node-live")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+root, "GPU_PROVIDER="+provider)
	if err := cmd.Run(); err == nil {
		nodeLive = true
		summary = append(summary, fmt.Sprintf("Node live (SSH + state): OK [%s]", displayProbeProvider(provider)))
	} else {
		summary = append(summary, fmt.Sprintf("Node live probe: failed [%s]", displayProbeProvider(provider)))
	}

	gatewayOK := false
	if nodeLive {
		port := os.Getenv("E2E_PORT")
		if port == "" {
			switch pool.ServiceTag {
			case models.OCRGPUServiceTag:
				port = "5006"
			default:
				port = "5011"
			}
		}
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		// Health is on the GPU node localhost — reach via SSH alias (Host e2e), not raw public IP (SG blocks direct).
		sshCmd := fmt.Sprintf("curl -sf --connect-timeout 10 'http://127.0.0.1:%s/health' >/dev/null", port)
		curl := exec.CommandContext(probeCtx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "e2e", sshCmd)
		if err := curl.Run(); err == nil {
			gatewayOK = true
			summary = append(summary, "On-node deploy health (via SSH): OK")
		} else {
			summary = append(summary, "On-node deploy health (via SSH): unreachable")
		}
	} else if pool.PublicIP != "" {
		summary = append(summary, "On-node deploy health: skipped (node not live)")
	} else {
		summary = append(summary, "On-node deploy health: no node")
	}

	providerNodeCount := 0
	providerNodes := []map[string]any{}
	listCmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts/gpu_provision_node.sh"), "list-nodes")
	listCmd.Dir = root
	listCmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+root, "GPU_PROVIDER="+provider)
	if out, err := listCmd.CombinedOutput(); err == nil {
		reCount := regexp.MustCompile(`count=(\d+)`)
		reNode := regexp.MustCompile(`id=([^\s]+)\s+status=([^\s]+)\s+ip=([^\s]+)\s+name=(.+)$`)
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if providerNodeCount == 0 {
				if m := reCount.FindStringSubmatch(line); len(m) == 2 {
					if n, convErr := strconv.Atoi(m[1]); convErr == nil {
						providerNodeCount = n
					}
				}
			}
			if m := reNode.FindStringSubmatch(line); len(m) == 5 {
				providerNodes = append(providerNodes, map[string]any{
					"id":       strings.TrimSpace(m[1]),
					"status":   strings.TrimSpace(m[2]),
					"publicIp": strings.TrimSpace(m[3]),
					"name":     strings.TrimSpace(m[4]),
				})
			}
		}
		if providerNodeCount == 0 {
			providerNodeCount = len(providerNodes)
		}
		summary = append(summary, fmt.Sprintf("Provider node list: %d node(s)", providerNodeCount))
	} else {
		summary = append(summary, "Provider node list: unavailable")
	}

	return map[string]any{
		"serviceTag":        pool.ServiceTag,
		"state":             pool.State,
		"provider":          provider,
		"providerLabel":     displayProbeProvider(provider),
		"nodeLive":          nodeLive,
		"gatewayHealth":     gatewayOK,
		"publicIp":          pool.PublicIP,
		"nodeId":            pool.NodeID,
		"providerNodeCount": providerNodeCount,
		"providerNodes":     providerNodes,
		"policySummary":     h.provisionConfigService.EffectiveSummary(cfg),
		"summary":           summary,
		"probedAt":          time.Now().UTC().Format(time.RFC3339),
	}
}

func displayProbeProvider(p string) string {
	switch strings.ToLower(p) {
	case "aws":
		return "AWS EC2"
	case "gcp":
		return "GCP"
	default:
		return "E2E Networks"
	}
}

func (h *GPUPoolHandler) AdminAbortProvision(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	if err := h.gpuPoolService.AdminAbortProvision(r.Context(), tag); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{"ok": true, "serviceTag": tag})
}

func (h *GPUPoolHandler) AdminRetryProvision(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	if err := h.gpuPoolService.AdminRetryProvision(r.Context(), tag); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, map[string]any{"ok": true, "serviceTag": tag})
}

func (h *GPUPoolHandler) AdminRecover(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	report, err := h.gpuPoolService.AdminRecover(r.Context(), tag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, report)
}

func (h *GPUPoolHandler) ListInventory(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	inv, err := h.gpuPoolService.ListInventory(r.Context(), tag, provider)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, inv)
}

func (h *GPUPoolHandler) AdminWarmStart(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	pool, err := h.gpuPoolService.AdminWarmStart(r.Context(), tag)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	utils.SendJSONResponse(w, http.StatusOK, h.enrichAdminPool(r.Context(), pool))
}

func (h *GPUPoolHandler) GetSupport(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "serviceTag")
	pools, err := h.gpuPoolService.ListAdmin(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}
	for _, p := range pools {
		if p == nil || p.ServiceTag != tag {
			continue
		}
		cfg, _ := h.provisionConfigService.GetOrDefault(r.Context(), tag)
		blocked := cfg != nil && cfg.BlocksNewSessions()
		canStart := !blocked && (p.State == models.GPUPoolStateIdle || p.State == models.GPUPoolStateReady)
		sessions := make([]models.GPUPoolSupportSessionView, 0, len(p.Sessions))
		for _, sess := range p.Sessions {
			sessions = append(sessions, models.GPUPoolSupportSessionView{
				UserID:                sess.UserID,
				StartedAt:             sess.StartedAt,
				StoppedAt:             sess.StoppedAt,
				CreditsCharged:        sess.CreditsCharged,
				CreditsStartupCharged: sess.CreditsStartupCharged,
				Active:                sess.StoppedAt == nil,
			})
		}
		raw := p.LastErrorRaw
		if raw == "" {
			raw = p.LastError
		}
		view := models.GPUPoolSupportView{
			ServiceTag:       p.ServiceTag,
			State:            p.State,
			Provider:         p.Provider,
			ProviderLabel:    displayProbeProvider(p.Provider),
			PublicIP:         p.PublicIP,
			RefCount:         p.RefCount,
			CanStart:         canStart,
			UserFacingError:  p.LastError,
			LastErrorRaw:     raw,
			MaintenanceBlock: blocked,
			Sessions:         sessions,
		}
		utils.SendJSONResponse(w, http.StatusOK, view)
		return
	}
	utils.SendErrorResponse(w, apperrors.NewAppError(apperrors.ErrNotFound, http.StatusNotFound, "gpu pool not found"))
}
