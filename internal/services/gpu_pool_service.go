package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
	apperrors "chi-mongo-backend/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
)

type GPUPoolService interface {
	Start(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error)
	Stop(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error)
	GetStatus(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error)
	ListAdmin(ctx context.Context) ([]*models.GPUPool, error)
	AdminWarmStart(ctx context.Context, serviceTag string) (*models.GPUPool, error)
	AdminShutdown(ctx context.Context, serviceTag string, immediate bool) (*models.GPUPool, error)
	AdminCancelGrace(ctx context.Context, serviceTag string) (*models.GPUPool, error)
	AdminExtendGrace(ctx context.Context, serviceTag string, extend time.Duration) (*models.GPUPool, error)
	AdminAbortProvision(ctx context.Context, serviceTag string) error
	AdminRetryProvision(ctx context.Context, serviceTag string) error
	AdminRecover(ctx context.Context, serviceTag string) (*models.GPUPoolRecoveryReport, error)
	NodeClaimedByOtherPool(ctx context.Context, serviceTag, nodeID, publicIP string) (*models.GPUPool, error)
	ListInventory(ctx context.Context, serviceTag, providerOverride string) (*models.GPUPoolInventory, error)
	ReconcileIdleWarmPools(ctx context.Context) error
	ReconcileOrphanPools(ctx context.Context) error
	ReconcileScheduledState(ctx context.Context) error
	HandleMeterTick(ctx context.Context, serviceTag, userID string) error
	HandleProvisionFailed(ctx context.Context, serviceTag string) error
	OnProvisionSkippedNoSessions(ctx context.Context, serviceTag string) error
	HandleProvisionNoSessions(ctx context.Context, serviceTag string, afterPipeline bool) (continueProvision bool, err error)
}

type gpuPoolService struct {
	poolRepo   repository.GPUPoolRepository
	jobSvc     JobService
	creditsSvc CreditsService
	policySvc  GPUProvisionConfigService
	cfg        *config.Config
}

func NewGPUPoolService(poolRepo repository.GPUPoolRepository, jobSvc JobService, creditsSvc CreditsService, policySvc GPUProvisionConfigService, cfg *config.Config) GPUPoolService {
	return &gpuPoolService{poolRepo: poolRepo, jobSvc: jobSvc, creditsSvc: creditsSvc, policySvc: policySvc, cfg: cfg}
}

func (s *gpuPoolService) resolveServiceName(serviceTag string) (string, error) {
	name, ok := models.ServiceTagToPipelineService[serviceTag]
	if !ok || name == "" {
		return "", apperrors.NewAppError(apperrors.ErrValidation, 400, "unsupported gpu pool service tag")
	}
	return name, nil
}

func (s *gpuPoolService) provisionKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:provision", serviceTag)
}

func (s *gpuPoolService) destroyKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:destroy", serviceTag)
}

func (s *gpuPoolService) autoGraceDestroyEnabled() bool {
	return strings.TrimSpace(os.Getenv("GPU_POOL_DISABLE_AUTO_GRACE_DESTROY")) != "1"
}

func (s *gpuPoolService) automicaRoot() string {
	candidates := []string{}
	if s.cfg != nil && s.cfg.Worker.AutomicaRoot != "" {
		candidates = append(candidates, s.cfg.Worker.AutomicaRoot)
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

func (s *gpuPoolService) runAutomationCmd(ctx context.Context, args ...string) (string, error) {
	return s.runAutomationCmdWithEnv(ctx, nil, args...)
}

func (s *gpuPoolService) runAutomationCmdWithEnv(ctx context.Context, extraEnv []string, args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("missing command")
	}
	root := s.automicaRoot()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "AUTOMICA_ROOT="+root)
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Env, extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type e2eNodeSnapshot struct {
	ID       string
	Name     string
	Status   string
	PublicIP string
}

func parseListNodesOutput(output string) (int, []e2eNodeSnapshot) {
	lines := strings.Split(output, "\n")
	reCount := regexp.MustCompile(`count=(\d+)`)
	reNode := regexp.MustCompile(`id=([^\s]+)\s+status=([^\s]+)\s+ip=([^\s]+)\s+name=(.+)$`)
	count := -1
	nodes := []e2eNodeSnapshot{}
	for _, line := range lines {
		if count < 0 {
			if m := reCount.FindStringSubmatch(line); len(m) == 2 {
				if n, err := strconv.Atoi(m[1]); err == nil {
					count = n
				}
			}
		}
		if m := reNode.FindStringSubmatch(line); len(m) == 5 {
			nodes = append(nodes, e2eNodeSnapshot{
				ID:       strings.TrimSpace(m[1]),
				Status:   strings.TrimSpace(m[2]),
				PublicIP: strings.TrimSpace(m[3]),
				Name:     strings.TrimSpace(m[4]),
			})
		}
	}
	if count < 0 {
		count = len(nodes)
	}
	return count, nodes
}

func parseAdoptOutput(output string) e2eNodeSnapshot {
	re := regexp.MustCompile(`id=([^\s]+)\s+name=([^\s]+)\s+ip=([^\s]+)\s+user=([^\s]+)`)
	if m := re.FindStringSubmatch(output); len(m) == 5 {
		return e2eNodeSnapshot{
			ID:       strings.TrimSpace(m[1]),
			Name:     strings.TrimSpace(m[2]),
			PublicIP: strings.TrimSpace(m[3]),
			Status:   "Running",
		}
	}
	return e2eNodeSnapshot{}
}

func displayRecoveryProvider(p string) string {
	switch strings.ToLower(p) {
	case "aws":
		return "AWS EC2"
	case "gcp":
		return "GCP"
	default:
		return "E2E Networks"
	}
}

func recoverySSHHost(provider string) string {
	if host := strings.TrimSpace(os.Getenv("GPU_SSH_HOST")); host != "" {
		return host
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "aws":
		return "vlm-aws"
	default:
		if host := strings.TrimSpace(os.Getenv("E2E_SSH_HOST")); host != "" {
			return host
		}
		return "e2e"
	}
}

func isAdoptableNodeStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "running", "pending", "creating", "starting", "active":
		return true
	default:
		return false
	}
}

func filterAdoptableNodes(nodes []e2eNodeSnapshot) []e2eNodeSnapshot {
	out := make([]e2eNodeSnapshot, 0, len(nodes))
	for _, n := range nodes {
		if isAdoptableNodeStatus(n.Status) {
			out = append(out, n)
		}
	}
	return out
}

func (s *gpuPoolService) recoveryAutomationEnv(ctx context.Context, serviceTag, provider string) []string {
	env := []string{
		"GPU_PROVIDER=" + provider,
		"GPU_PROVISION_PRIMARY=" + provider,
		"GPU_SERVICE_TAG=" + serviceTag,
		"GPU_SSH_HOST=" + recoverySSHHost(provider),
	}
	if s.policySvc != nil {
		if cfg, err := s.policySvc.GetOrDefault(ctx, serviceTag); err == nil && cfg != nil {
			env = append(env, s.policySvc.WorkerEnv(cfg)...)
			// Ensure probe provider wins over WorkerEnv primary when pool.provider differs.
			env = append(env, "GPU_PROVIDER="+provider, "GPU_PROVISION_PRIMARY="+provider)
		}
	}
	return env
}

func (s *gpuPoolService) hintNodesFromLocalState(provider, serviceTag string) []e2eNodeSnapshot {
	reg := filepath.Join(s.automicaRoot(), "services/pipeline/registry")
	patterns := []string{
		filepath.Join(reg, ".gpu_provision_state*.json"),
		filepath.Join(reg, ".e2e_provision_state.json"),
	}
	var nodes []e2eNodeSnapshot
	seen := map[string]bool{}
	wantTag := models.CanonicalGPUServiceTag(serviceTag)
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			// Minimal JSON extract without pulling encoding/json into hot path repeatedly —
			// use a tiny unmarshal via existing stdlib.
			var st struct {
				Provider   string `json:"provider"`
				NodeID     string `json:"node_id"`
				PublicIP   string `json:"public_ip"`
				Name       string `json:"name"`
				Status     string `json:"status"`
				ServiceTag string `json:"service_tag"`
			}
			if err := jsonUnmarshalState(raw, &st); err != nil {
				continue
			}
			if strings.TrimSpace(st.NodeID) == "" && strings.TrimSpace(st.PublicIP) == "" {
				continue
			}
			stProvider := strings.ToLower(strings.TrimSpace(st.Provider))
			if stProvider == "" {
				stProvider = "e2e"
			}
			if stProvider != strings.ToLower(provider) {
				continue
			}
			// Shared state file: only adopt hints that belong to this pool (or legacy untagged).
			if tag := strings.TrimSpace(st.ServiceTag); tag != "" && models.CanonicalGPUServiceTag(tag) != wantTag {
				continue
			}
			if !isAdoptableNodeStatus(st.Status) && st.Status != "" {
				continue
			}
			key := st.NodeID + "|" + st.PublicIP
			if seen[key] {
				continue
			}
			seen[key] = true
			status := st.Status
			if status == "" {
				status = "running"
			}
			nodes = append(nodes, e2eNodeSnapshot{
				ID:       st.NodeID,
				Name:     st.Name,
				Status:   status,
				PublicIP: st.PublicIP,
			})
		}
	}
	return nodes
}

func jsonUnmarshalState(raw []byte, dest any) error {
	return json.Unmarshal(raw, dest)
}

func (s *gpuPoolService) probeRecoveryTruth(ctx context.Context, pool *models.GPUPool) (*models.GPUPoolRecoveryReport, error) {
	if pool == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}
	cfg := s.policyFor(ctx, pool.ServiceTag)
	provider := strings.TrimSpace(pool.Provider)
	if provider == "" && cfg != nil {
		provider = string(cfg.Infrastructure.PrimaryProvider)
	}
	if provider == "" {
		provider = "e2e"
	}
	provider = strings.ToLower(provider)
	report := &models.GPUPoolRecoveryReport{
		ServiceTag:    pool.ServiceTag,
		State:         pool.State,
		Provider:      provider,
		ProviderLabel: displayRecoveryProvider(provider),
		Notes:         []string{},
		ProbedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	sshHost := recoverySSHHost(provider)
	autoEnv := s.recoveryAutomationEnv(ctx, pool.ServiceTag, provider)

	sshCmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", sshHost, "echo ok")
	sshOut, sshErr := sshCmd.CombinedOutput()
	report.SSHReachable = sshErr == nil && strings.Contains(string(sshOut), "ok")
	if sshErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("ssh %s unreachable", sshHost))
	} else {
		report.Notes = append(report.Notes, fmt.Sprintf("ssh %s reachable", sshHost))
		adoptOut, adoptErr := s.runAutomationCmdWithEnv(ctx, autoEnv, "bash", filepath.Join(s.automicaRoot(), "scripts/gpu_provision_node.sh"), "adopt")
		if adoptErr == nil {
			adopt := parseAdoptOutput(adoptOut)
			if adopt.ID != "" || adopt.PublicIP != "" {
				report.ProviderNodes = append(report.ProviderNodes, models.GPUPoolRecoveryNode{
					ID:       adopt.ID,
					Name:     adopt.Name,
					Status:   adopt.Status,
					PublicIP: adopt.PublicIP,
				})
				if report.ProviderNodeCount == 0 {
					report.ProviderNodeCount = 1
				}
			}
			report.Notes = append(report.Notes, "node adopted from live ssh alias")
		} else {
			report.Notes = append(report.Notes, "live ssh alias could not be adopted")
		}
	}

	for _, hint := range s.hintNodesFromLocalState(provider, pool.ServiceTag) {
		report.Notes = append(report.Notes, fmt.Sprintf("local state hint id=%s ip=%s", hint.ID, hint.PublicIP))
		report.ProviderNodes = append(report.ProviderNodes, models.GPUPoolRecoveryNode{
			ID:       hint.ID,
			Name:     hint.Name,
			Status:   hint.Status,
			PublicIP: hint.PublicIP,
		})
	}

	listOut, listErr := s.runAutomationCmdWithEnv(ctx, autoEnv, "bash", filepath.Join(s.automicaRoot(), "scripts/gpu_provision_node.sh"), "list-nodes")
	if listErr != nil {
		report.Notes = append(report.Notes, "provider node list unavailable")
	} else {
		_, nodes := parseListNodesOutput(listOut)
		adoptable := filterAdoptableNodes(nodes)
		report.ProviderNodeCount = len(adoptable)
		// Prefer provider inventory over earlier hints when list succeeds.
		if len(adoptable) > 0 {
			report.ProviderNodes = nil
			for _, node := range adoptable {
				report.ProviderNodes = append(report.ProviderNodes, models.GPUPoolRecoveryNode{
					ID:       node.ID,
					Name:     node.Name,
					Status:   node.Status,
					PublicIP: node.PublicIP,
				})
			}
		}
	}

	// Deduplicate provider nodes by id/ip.
	if len(report.ProviderNodes) > 1 {
		uniq := make([]models.GPUPoolRecoveryNode, 0, len(report.ProviderNodes))
		seen := map[string]bool{}
		for _, n := range report.ProviderNodes {
			key := strings.TrimSpace(n.ID) + "|" + strings.TrimSpace(n.PublicIP)
			if seen[key] {
				continue
			}
			seen[key] = true
			uniq = append(uniq, n)
		}
		report.ProviderNodes = uniq
		if report.ProviderNodeCount < len(uniq) {
			report.ProviderNodeCount = len(uniq)
		}
	}

	switch {
	case report.SSHReachable:
		report.Action = "adopt-ssh"
		report.Recovered = true
		report.Message = "SSH alias is live; reuse can adopt the existing node."
	case report.ProviderNodeCount == 1:
		report.Action = "adopt-provider"
		report.Recovered = true
		report.Message = "Provider has exactly one node; reuse can adopt it."
	case report.ProviderNodeCount == 0:
		report.Action = "recreate-required"
		report.Message = "No reusable provider node exists yet."
	default:
		report.Action = "conflict-multiple-nodes"
		report.Message = "Multiple provider nodes exist; recreate/cleanup is required before reuse."
	}
	return report, nil
}

// ListInventory returns live provider nodes for a pool tag (source: gpu_provision_node.sh list-nodes).
func (s *gpuPoolService) ListInventory(ctx context.Context, serviceTag, providerOverride string) (*models.GPUPoolInventory, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}
	if _, err := s.resolveServiceName(serviceTag); err != nil {
		return nil, err
	}
	pool, _ := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	cfg := s.policyFor(ctx, serviceTag)

	provider := strings.ToLower(strings.TrimSpace(providerOverride))
	if provider == "" && pool != nil {
		provider = strings.ToLower(strings.TrimSpace(pool.Provider))
	}
	if provider == "" && cfg != nil {
		provider = strings.ToLower(string(cfg.Infrastructure.PrimaryProvider))
	}
	if provider == "" {
		provider = "e2e"
	}

	inv := &models.GPUPoolInventory{
		ServiceTag:    serviceTag,
		Provider:      provider,
		ProviderLabel: displayRecoveryProvider(provider),
		SSHHost:       recoverySSHHost(provider),
		Source:        "provider-list-nodes",
		Notes:         []string{},
		ProviderNodes: []models.GPUPoolRecoveryNode{},
		ProbedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if pool != nil {
		inv.MongoState = pool.State
		inv.MongoNodeID = pool.NodeID
		inv.MongoPublicIP = pool.PublicIP
	}

	sshCmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", inv.SSHHost, "echo ok")
	sshOut, sshErr := sshCmd.CombinedOutput()
	inv.SSHReachable = sshErr == nil && strings.Contains(string(sshOut), "ok")
	if sshErr != nil {
		inv.Notes = append(inv.Notes, fmt.Sprintf("ssh %s unreachable", inv.SSHHost))
	} else {
		inv.Notes = append(inv.Notes, fmt.Sprintf("ssh %s reachable", inv.SSHHost))
	}

	autoEnv := s.recoveryAutomationEnv(ctx, serviceTag, provider)
	listOut, listErr := s.runAutomationCmdWithEnv(ctx, autoEnv, "bash", filepath.Join(s.automicaRoot(), "scripts/gpu_provision_node.sh"), "list-nodes")
	if listErr != nil {
		inv.Notes = append(inv.Notes, "provider list-nodes failed: "+sanitizeInventoryErr(listErr.Error()))
		preview := strings.TrimSpace(listOut)
		if len(preview) > 800 {
			preview = preview[:800] + "…"
		}
		inv.RawPreview = preview
		return inv, nil
	}
	_, nodes := parseListNodesOutput(listOut)
	for _, n := range nodes {
		inv.ProviderNodes = append(inv.ProviderNodes, models.GPUPoolRecoveryNode{
			ID:       n.ID,
			Name:     n.Name,
			Status:   n.Status,
			PublicIP: n.PublicIP,
		})
	}
	inv.ProviderNodeCount = len(inv.ProviderNodes)
	preview := strings.TrimSpace(listOut)
	if len(preview) > 1200 {
		preview = preview[:1200] + "…"
	}
	inv.RawPreview = preview
	inv.Notes = append(inv.Notes, fmt.Sprintf("listed %d provider node(s)", inv.ProviderNodeCount))
	return inv, nil
}

func sanitizeInventoryErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 240 {
		return msg[:240] + "…"
	}
	return msg
}

func (s *gpuPoolService) graceDestroyKey(serviceTag string) string {
	return fmt.Sprintf("gpu-pool:%s:grace_destroy", serviceTag)
}

func (s *gpuPoolService) gracePeriodForPool(ctx context.Context, pool *models.GPUPool) time.Duration {
	if pool == nil {
		return s.gracePeriodFor(ctx, "")
	}
	return s.gracePeriodFor(ctx, pool.ServiceTag)
}

func (s *gpuPoolService) destroyAtForPool(ctx context.Context, pool *models.GPUPool) *time.Time {
	if pool == nil || pool.State != models.GPUPoolStateDraining || !pool.HasScheduledGraceDestroy() {
		return nil
	}
	if pool.DestroyAt != nil {
		return pool.DestroyAt
	}
	if pool.DrainStartedAt == nil {
		return nil
	}
	t := pool.DrainStartedAt.Add(s.gracePeriodForPool(ctx, pool))
	return &t
}

func (s *gpuPoolService) enqueueGraceDestroyAt(ctx context.Context, serviceTag, serviceName string, runAfter time.Time) error {
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolGraceDestroy, EnqueueJobOptions{
		IdempotencyKey: s.graceDestroyKey(serviceTag),
		RunAfter:       runAfter,
		Payload: map[string]any{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
		},
		MaxAttempts: s.destroyMaxAttempts(ctx, serviceTag),
	})
	if err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrValidation {
			_, rescheduleErr := s.jobSvc.ReschedulePendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag), runAfter)
			return rescheduleErr
		}
		return err
	}
	return nil
}

func (s *gpuPoolService) enqueueGraceDestroy(ctx context.Context, serviceTag, serviceName string) error {
	return s.enqueueGraceDestroyAt(ctx, serviceTag, serviceName, time.Now().UTC().Add(s.gracePeriodFor(ctx, serviceTag)))
}

func (s *gpuPoolService) adminDestroyInProgress(ctx context.Context, serviceTag string) (bool, error) {
	running, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag)
	if err != nil {
		return false, err
	}
	if running {
		return true, nil
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.destroyKey(serviceTag))
	return active, err
}

// maybeScheduleUserGraceDestroy runs after Stop when the last session ends on any warm node.
func (s *gpuPoolService) maybeScheduleUserGraceDestroy(ctx context.Context, pool *models.GPUPool, serviceName string) error {
	if pool == nil || pool.RefCount > 0 || !pool.TeardownOnUserStop() {
		return nil
	}
	if !s.autoGraceDestroyEnabled() {
		return nil
	}
	if pool.AdminWarmHold {
		return nil
	}
	if pool.NodeID == "" && pool.PublicIP == "" {
		return nil
	}
	if destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag); err != nil {
		return err
	} else if destroying {
		return nil
	}

	now := time.Now().UTC()
	updated, err := s.poolRepo.Update(ctx, pool.ServiceTag, map[string]any{
		"state":          models.GPUPoolStateDraining,
		"drainStartedAt": now,
		"destroyAt":      now.Add(s.gracePeriodFor(ctx, pool.ServiceTag)),
		"drainReason":    models.GPUPoolDrainReasonUserGrace,
	})
	if err != nil {
		return err
	}
	_ = updated
	return s.enqueueGraceDestroy(ctx, pool.ServiceTag, serviceName)
}

func (s *gpuPoolService) afterSessionStopped(ctx context.Context, serviceTag string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	if pool == nil || pool.RefCount > 0 {
		return nil
	}
	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil
	}
	switch pool.State {
	case models.GPUPoolStateReady:
		return s.maybeScheduleUserGraceDestroy(ctx, pool, serviceName)
	case models.GPUPoolStateProvisioning:
		return s.afterEarlyStopDuringProvision(ctx, pool, serviceName)
	default:
		return nil
	}
}

// reconcileIdleWarmPool schedules grace teardown for ready pools with no sessions but a live VM.
func (s *gpuPoolService) reconcileIdleWarmPool(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.RefCount > 0 || pool.State != models.GPUPoolStateReady {
		return pool, nil
	}
	if pool.AdminWarmHold {
		return pool, nil
	}
	if !pool.TeardownOnUserStop() {
		return pool, nil
	}
	serviceName, err := s.resolveServiceName(pool.ServiceTag)
	if err != nil {
		return pool, nil
	}
	if err := s.maybeScheduleUserGraceDestroy(ctx, pool, serviceName); err != nil {
		return pool, err
	}
	return s.poolRepo.GetByServiceTag(ctx, pool.ServiceTag)
}

func (s *gpuPoolService) ReconcileIdleWarmPools(ctx context.Context) error {
	pools, err := s.poolRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if pool == nil {
			continue
		}
		if _, err := s.reconcileIdleWarmPool(ctx, pool); err != nil {
			return err
		}
	}
	return nil
}

func poolNeedsOrphanRecover(pool *models.GPUPool) bool {
	if pool == nil || pool.RefCount > 0 {
		return false
	}
	switch pool.State {
	case models.GPUPoolStateIdle, models.GPUPoolStateFailed:
		// ok
	default:
		return false
	}
	return strings.TrimSpace(pool.NodeID) == "" && strings.TrimSpace(pool.PublicIP) == ""
}

// ReconcileOrphanPools reattaches idle/failed empty Mongo pools when the provider still has a live VM.
func (s *gpuPoolService) ReconcileOrphanPools(ctx context.Context) error {
	pools, err := s.poolRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if !poolNeedsOrphanRecover(pool) {
			continue
		}
		if destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag); err != nil {
			return err
		} else if destroying {
			continue
		}
		report, recoverErr := s.AdminRecover(ctx, pool.ServiceTag)
		if recoverErr != nil {
			log.Printf("gpu orphan recover %s: %v", pool.ServiceTag, recoverErr)
			continue
		}
		if report != nil && report.Recovered && (report.Action == "adopt-ssh" || report.Action == "adopt-provider") {
			log.Printf("gpu orphan recover %s: action=%s provider=%s nodes=%d",
				pool.ServiceTag, report.Action, report.Provider, report.ProviderNodeCount)
		} else if report != nil {
			log.Printf("gpu orphan recover %s: no reattach action=%s msg=%s",
				pool.ServiceTag, report.Action, report.Message)
		}
	}
	return nil
}

func (s *gpuPoolService) ReconcileScheduledState(ctx context.Context) error {
	// Reattach billing orphans before idle-warm grace so admin/Try API see ready truth.
	if err := s.ReconcileOrphanPools(ctx); err != nil {
		return err
	}
	if err := s.ReconcileIdleWarmPools(ctx); err != nil {
		return err
	}
	root := s.automicaRoot()
	script := filepath.Join(root, "scripts", "run_gpu_pool_reconcile_jobs.sh")
	if _, err := os.Stat(script); err != nil {
		// Backward-compatible fallback: older trees only had the .mjs.
		legacy := filepath.Join(root, "scripts", "gpu_pool_reconcile_jobs.mjs")
		if _, legacyErr := os.Stat(legacy); legacyErr != nil {
			return err
		}
		out, runErr := s.runAutomationCmd(ctx, "node", legacy)
		if strings.TrimSpace(out) != "" {
			log.Printf("gpu pool reconcile:\n%s", strings.TrimSpace(out))
		}
		return runErr
	}
	out, err := s.runAutomationCmd(ctx, "bash", script)
	if strings.TrimSpace(out) != "" {
		log.Printf("gpu pool reconcile:\n%s", strings.TrimSpace(out))
	}
	return err
}

// cancelUserGraceIfResuming clears user-grace draining so Start Testing can reuse the node.
func (s *gpuPoolService) cancelResumeGraceIfResuming(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateDraining {
		return pool, nil
	}
	destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag)
	if err != nil {
		return pool, err
	}
	if destroying {
		return pool, nil
	}
	if pool.DrainReason != models.GPUPoolDrainReasonUserGrace &&
		pool.DrainReason != models.GPUPoolDrainReasonFailedBootstrap {
		return pool, nil
	}
	updates := map[string]any{
		"drainStartedAt": nil,
		"destroyAt":      nil,
		"drainReason":    "",
		"adminWarmHold":  false,
	}
	if pool.DrainReason == models.GPUPoolDrainReasonFailedBootstrap {
		updates["state"] = models.GPUPoolStateFailed
	} else {
		updates["state"] = models.GPUPoolStateReady
	}
	return s.cancelScheduledGrace(ctx, pool, updates)
}

func (s *gpuPoolService) enqueueDestroy(ctx context.Context, serviceTag, serviceName string) error {
	key := s.destroyKey(serviceTag)
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolDestroy, EnqueueJobOptions{
		IdempotencyKey: key,
		Payload: map[string]any{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
		},
		MaxAttempts: s.destroyMaxAttempts(ctx, serviceTag),
	})
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrValidation {
		active, activeErr := s.jobSvc.HasActiveByIdempotencyKey(ctx, key)
		if activeErr != nil {
			return activeErr
		}
		running, runErr := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag)
		if runErr != nil {
			return runErr
		}
		if active || running {
			return nil
		}
	}
	return err
}

// adminAbortPoolNoNode cancels an in-flight boot when no E2E node exists yet (or metadata was lost).
func (s *gpuPoolService) adminAbortPoolNoNode(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.provisionKey(serviceTag))
	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))

	failUpdate := s.poolErrorUpdate(ctx, serviceTag, "admin terminated before GPU was ready")
	failUpdate["state"] = models.GPUPoolStateFailed
	failUpdate["adminWarmHold"] = false
	failUpdate["drainReason"] = ""
	failUpdate["drainStartedAt"] = nil
	if _, err := s.poolRepo.Update(ctx, serviceTag, failUpdate); err != nil {
		return nil, err
	}
	_ = s.refundFailedPoolSessions(ctx, serviceTag)

	return s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":            models.GPUPoolStateIdle,
		"lastError":        "",
		"lastErrorRaw":     "",
		"refCount":         0,
		"nodeId":           "",
		"publicIp":         "",
		"previousPublicIp": "",
		"readyAt":          nil,
	})
}

func (s *gpuPoolService) enqueueProvision(ctx context.Context, serviceTag, serviceName string) error {
	_, err := s.jobSvc.Enqueue(ctx, models.JobTypeGPUPoolProvision, EnqueueJobOptions{
		IdempotencyKey: s.provisionKey(serviceTag),
		Payload: map[string]any{
			"serviceTag":  serviceTag,
			"serviceName": serviceName,
		},
		MaxAttempts: s.provisionMaxAttempts(ctx, serviceTag),
	})
	if err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrValidation {
			return nil
		}
		return err
	}
	return nil
}

// ensureProvisionJob re-queues when pool is provisioning but the worker job was lost
// (e.g. dead idempotency row blocked a fresh enqueue).
func (s *gpuPoolService) ensureProvisionJob(ctx context.Context, pool *models.GPUPool) error {
	if pool == nil || pool.RefCount == 0 || pool.State != models.GPUPoolStateProvisioning {
		return nil
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(pool.ServiceTag))
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	return s.enqueueProvision(ctx, pool.ServiceTag, pool.ServiceName)
}

// reconcilePool re-enqueues destroy for draining pools stuck without an active job
// (e.g. worker restart mid-destroy). Never clears node metadata while a VM may still exist.
func (s *gpuPoolService) reconcilePool(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateDraining || pool.RefCount > 0 {
		return pool, nil
	}

	if pool.NodeID == "" && pool.PublicIP == "" {
		return s.poolRepo.Update(ctx, pool.ServiceTag, map[string]any{
			"state":            models.GPUPoolStateIdle,
			"nodeId":           "",
			"publicIp":         "",
			"previousPublicIp": "",
			"drainStartedAt":   nil,
			"destroyAt":        nil,
			"drainReason":      "",
			"readyAt":          nil,
		})
	}

	const drainRetryAfter = 3 * time.Minute
	deadline := time.Now().UTC()
	if pool.DrainStartedAt != nil {
		deadline = pool.DrainStartedAt.Add(drainRetryAfter)
	}
	if time.Now().UTC().Before(deadline) {
		return pool, nil
	}

	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.destroyKey(pool.ServiceTag))
	if err != nil {
		return pool, err
	}
	running, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, pool.ServiceTag)
	if err != nil {
		return pool, err
	}
	if active || running {
		return pool, nil
	}

	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(pool.ServiceTag))
	if err := s.enqueueDestroy(ctx, pool.ServiceTag, pool.ServiceName); err != nil {
		return pool, err
	}
	return pool, nil
}

// reconcileStuckProvision marks provisioning pools failed when the worker job is gone or
// exceeded wall-clock limits (E2E Creating/Deleting stall, zombie running job).
// When refCount=0 (user stopped mid-provision), clears stuck provisioning so Start can retry.
func (s *gpuPoolService) reconcileStuckProvision(ctx context.Context, pool *models.GPUPool) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateProvisioning {
		return pool, nil
	}

	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(pool.ServiceTag))
	if err != nil {
		return pool, err
	}

	if pool.RefCount == 0 {
		if active {
			return pool, nil
		}
		if s.poolInReconnectWindow(ctx, pool) {
			return pool, nil
		}
		update := map[string]any{
			"state": models.GPUPoolStateIdle,
		}
		if pool.LastError != "" {
			update["state"] = models.GPUPoolStateFailed
		}
		return s.poolRepo.Update(ctx, pool.ServiceTag, update)
	}

	age := time.Since(pool.UpdatedAt)
	noJobFailAfter, zombieFailAfter := s.stuckProvisionThresholds(ctx, pool.ServiceTag)
	if active && age < zombieFailAfter {
		return pool, nil
	}
	if !active && age < noJobFailAfter {
		return pool, nil
	}

	stuckUpdate := s.poolErrorUpdate(ctx, pool.ServiceTag, "provision stalled beyond time limit")
	stuckUpdate["state"] = models.GPUPoolStateFailed
	updated, err := s.poolRepo.Update(ctx, pool.ServiceTag, stuckUpdate)
	if err != nil {
		return pool, err
	}
	if updated.State == models.GPUPoolStateFailed {
		_ = s.refundFailedPoolSessions(ctx, pool.ServiceTag)
		updated, _ = s.poolRepo.GetByServiceTag(ctx, pool.ServiceTag)
	}
	return updated, nil
}

func (s *gpuPoolService) HandleProvisionFailed(ctx context.Context, serviceTag string) error {
	if err := s.refundFailedPoolSessions(ctx, serviceTag); err != nil {
		return err
	}
	if !s.autoGraceDestroyEnabled() {
		return nil
	}
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil || pool == nil {
		return err
	}
	if pool.RefCount > 0 {
		return nil
	}
	if pool.NodeID == "" && pool.PublicIP == "" {
		return nil
	}
	serviceName := pool.ServiceName
	if serviceName == "" {
		serviceName, err = s.resolveServiceName(serviceTag)
		if err != nil {
			return nil
		}
	}
	now := time.Now().UTC()
	destroyAt := now.Add(s.gracePeriodFor(ctx, serviceTag))
	if _, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":          models.GPUPoolStateDraining,
		"drainStartedAt": now,
		"destroyAt":      destroyAt,
		"drainReason":    models.GPUPoolDrainReasonFailedBootstrap,
		"adminWarmHold":  false,
	}); err != nil {
		return err
	}
	return s.enqueueGraceDestroyAt(ctx, serviceTag, serviceName, destroyAt)
}

// OnProvisionSkippedNoSessions handles worker entry when refCount=0 before bootstrap starts.
func (s *gpuPoolService) OnProvisionSkippedNoSessions(ctx context.Context, serviceTag string) error {
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil || pool == nil || pool.RefCount > 0 {
		return err
	}
	if pool.State == models.GPUPoolStateDraining && pool.DrainReason == models.GPUPoolDrainReasonUserGrace {
		return nil
	}
	if pool.State != models.GPUPoolStateProvisioning {
		return nil
	}
	if s.poolInReconnectWindow(ctx, pool) {
		return nil
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(serviceTag))
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	update := map[string]any{"state": models.GPUPoolStateIdle}
	if pool.LastError != "" {
		update["state"] = models.GPUPoolStateFailed
	}
	_, err = s.poolRepo.Update(ctx, serviceTag, update)
	return err
}

type gpuPoolStatusOpts struct {
	freshSessionStart bool
}

// gpuPoolReattachedSession is true when the client is reconnecting to an existing session
// (status poll or idempotent Start), not immediately after creating a new session.
func gpuPoolReattachedSession(userActive bool, hasActiveSession bool, freshSessionStart bool) bool {
	return userActive && hasActiveSession && !freshSessionStart
}

func (s *gpuPoolService) toStatus(ctx context.Context, pool *models.GPUPool, userID string, opts ...gpuPoolStatusOpts) *models.GPUPoolStatusResponse {
	var o gpuPoolStatusOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	userActive := false
	var activeSess *models.GPUPoolSession
	for i := range pool.Sessions {
		if pool.Sessions[i].UserID == userID && pool.Sessions[i].StoppedAt == nil {
			userActive = true
			activeSess = &pool.Sessions[i]
			break
		}
	}
	creditsCharged, billingActive, endReason := s.sessionStatusFields(pool, userID)
	startupCharged, gpuTimeCharged := 0, 0
	if activeSess != nil {
		startupCharged, _, gpuTimeCharged = sessionCreditBreakdown(activeSess)
	} else if stopped := lastStoppedSession(pool, userID); stopped != nil {
		startupCharged, _, gpuTimeCharged = sessionCreditBreakdown(stopped)
	}
	interval := s.meterInterval()
	lastError := s.sanitizePoolError(ctx, pool.ServiceTag, pool.LastError)
	if !userActive && pool.RefCount == 0 &&
		(pool.State == models.GPUPoolStateFailed || pool.State == models.GPUPoolStateIdle) {
		lastError = ""
	}
	reconnectEligible, reconnectUntil, _ := s.reconnectWindow(ctx, pool, userID)
	return &models.GPUPoolStatusResponse{
		ServiceTag:                   pool.ServiceTag,
		ServiceName:                  pool.ServiceName,
		State:                        pool.State,
		RefCount:                     pool.RefCount,
		PublicIP:                     pool.PublicIP,
		NodeID:                       pool.NodeID,
		ReadyAt:                      pool.ReadyAt,
		DrainStartedAt:               pool.DrainStartedAt,
		DrainReason:                  pool.DrainReason,
		DestroyAt:                    s.destroyAtForPool(ctx, pool),
		GracePeriodSec:               s.gracePeriodSecFor(ctx, pool.ServiceTag),
		LastError:                    lastError,
		PollURL:                      fmt.Sprintf("/api/v1/gpu-pool/status?serviceTag=%s", pool.ServiceTag),
		UserActive:                   userActive,
		CreditsChargedSession:        creditsCharged,
		CreditsStartupChargedSession: startupCharged,
		CreditsGpuTimeSession:        gpuTimeCharged,
		CreditsPerMinute:             s.creditsPerMinute(),
		StartupCredits:               s.startupCredits(),
		MinCreditsToStart:            s.minStartCredits(),
		MeterIntervalSec:             s.meterIntervalSec(),
		NextMeterChargeAt:            nextMeterChargeAt(activeSess, interval),
		BillingActive:                billingActive,
		ReattachedSession:            gpuPoolReattachedSession(userActive, activeSess != nil, o.freshSessionStart),
		SessionEndReason:             endReason,
		ReconnectEligible:            reconnectEligible,
		ReconnectUntil:               reconnectUntil,
	}
}

func (s *gpuPoolService) Start(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	if userID == "" || serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "userId and serviceTag are required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}

	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.BlocksNewSessions() {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 503, cfg.MaintenanceUserMessage())
	}

	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	existing, _ := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if existing != nil {
		reconciled, err := s.reconcilePool(ctx, existing)
		if err != nil {
			return nil, err
		}
		reconciled, err = s.reconcileStuckProvision(ctx, reconciled)
		if err != nil {
			return nil, err
		}
		reconciled, err = s.reconcileUserSession(ctx, reconciled, userID)
		if err != nil {
			return nil, err
		}
		existing = reconciled
	}

	if existing != nil {
		resumed, err := s.cancelResumeGraceIfResuming(ctx, existing)
		if err != nil {
			return nil, err
		}
		existing = resumed
	}

	graceKey := s.graceDestroyKey(serviceTag)
	reconnectKey := s.reconnectDestroyKey(serviceTag)
	if existing != nil && existing.State == models.GPUPoolStateDraining {
		msg := "The test resource is retrying shutdown. Wait a moment and try Start Testing again."
		switch existing.DrainReason {
		case models.GPUPoolDrainReasonAdminGrace:
			msg = "This test resource is shutting down. Contact support."
		case models.GPUPoolDrainReasonUserGrace:
			msg = "This test resource is restarting from standby. Wait a moment and try Start Testing again."
		case models.GPUPoolDrainReasonFailedBootstrap:
			msg = "This test resource is recovering from a failed start. Wait a moment and try Start Testing again."
		}
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, msg)
	}
	if existing != nil && existing.State == models.GPUPoolStateReady {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, graceKey)
	}
	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, reconnectKey)

	if destroying, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag); err != nil {
		return nil, err
	} else if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409,
			"Resource is shutting down. Wait a minute and try Start Testing again.")
	}

	alreadyActive := existing != nil && activeSession(existing, userID) != nil
	if !alreadyActive {
		if err := s.checkStartCredits(ctx, userID, serviceTag); err != nil {
			return nil, err
		}
	}

	pool, provisionNeeded, err := s.poolRepo.StartSession(ctx, serviceTag, userID)
	if err != nil {
		return nil, err
	}

	if !alreadyActive {
		if err := s.chargeSessionStartup(ctx, serviceTag, userID); err != nil {
			_, _ = s.poolRepo.StopSession(ctx, serviceTag, userID)
			return nil, err
		}
	}

	if pool.State == models.GPUPoolStateReady {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, graceKey)
	}

	if provisionNeeded {
		if err := s.enqueueProvision(ctx, serviceTag, serviceName); err != nil {
			return nil, err
		}
	} else if pool.State == models.GPUPoolStateProvisioning && pool.RefCount > 0 {
		if err := s.ensureProvisionJob(ctx, pool); err != nil {
			return nil, err
		}
	}

	updated, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if err := s.ensureBillingStarted(ctx, updated, userID); err != nil {
		return nil, err
	}
	if err := s.ensureMeterTickScheduled(ctx, updated, userID); err != nil {
		return nil, err
	}
	updated, err = s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	return s.toStatus(ctx, updated, userID, gpuPoolStatusOpts{freshSessionStart: !alreadyActive}), nil
}

func (s *gpuPoolService) Stop(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	if userID == "" || serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "userId and serviceTag are required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}

	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	if err := s.finalizeBillingOnStop(ctx, serviceTag, userID); err != nil {
		if appErr, ok := err.(*apperrors.AppError); ok && appErr.Type == apperrors.ErrInsufficientCredits {
			// Best-effort stop even if final partial minute cannot be paid.
			s.cancelMeterTick(ctx, serviceTag, userID)
		} else {
			return nil, err
		}
	}

	pool, err := s.poolRepo.StopSession(ctx, serviceTag, userID)
	if err != nil {
		return nil, err
	}

	if pool.RefCount == 0 {
		if err := s.afterSessionStopped(ctx, serviceTag); err != nil {
			return nil, err
		}
		pool, err = s.poolRepo.GetByServiceTag(ctx, serviceTag)
		if err != nil {
			return nil, err
		}
	}

	return s.toStatus(ctx, pool, userID), nil
}

// cancelScheduledGrace clears a grace teardown and returns the pool to ready (user Start or admin override).
func (s *gpuPoolService) cancelScheduledGrace(ctx context.Context, pool *models.GPUPool, updates bson.M) (*models.GPUPool, error) {
	if pool == nil || pool.State != models.GPUPoolStateDraining {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "pool is not draining")
	}
	if !pool.HasScheduledGraceDestroy() {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "no scheduled grace teardown to cancel")
	}
	destroying, err := s.adminDestroyInProgress(ctx, pool.ServiceTag)
	if err != nil {
		return nil, err
	}
	if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "immediate destroy already in progress")
	}

	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(pool.ServiceTag))
	if updates == nil {
		updates = bson.M{}
	}
	if _, ok := updates["state"]; !ok {
		updates["state"] = models.GPUPoolStateReady
	}
	if _, ok := updates["drainStartedAt"]; !ok {
		updates["drainStartedAt"] = nil
	}
	if _, ok := updates["destroyAt"]; !ok {
		updates["destroyAt"] = nil
	}
	if _, ok := updates["drainReason"]; !ok {
		updates["drainReason"] = ""
	}
	if _, ok := updates["adminWarmHold"]; !ok {
		updates["adminWarmHold"] = true
	}
	return s.poolRepo.Update(ctx, pool.ServiceTag, updates)
}

func (s *gpuPoolService) AdminCancelGrace(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}
	if pool.State == models.GPUPoolStateDraining && pool.HasScheduledGraceDestroy() {
		return s.cancelScheduledGrace(ctx, pool, bson.M{
			"state":          models.GPUPoolStateReady,
			"drainStartedAt": nil,
			"destroyAt":      nil,
			"drainReason":    "",
			"adminWarmHold":  true,
		})
	}
	if pool.State == models.GPUPoolStateReady && pool.TeardownOnUserStop() {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))
		return s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"adminWarmHold": true,
		})
	}
	return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "pool is not eligible for warm hold")
}

func (s *gpuPoolService) AdminWarmStart(ctx context.Context, serviceTag string) (*models.GPUPool, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}
	if cfg := s.policyFor(ctx, serviceTag); cfg != nil && cfg.BlocksNewSessions() {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 503, cfg.MaintenanceUserMessage())
	}
	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}

	if pool.State == models.GPUPoolStateDraining && pool.HasScheduledGraceDestroy() {
		return s.cancelScheduledGrace(ctx, pool, bson.M{
			"state":          models.GPUPoolStateReady,
			"drainStartedAt": nil,
			"destroyAt":      nil,
			"drainReason":    "",
			"adminWarmHold":  true,
		})
	}
	if pool.State == models.GPUPoolStateReady {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))
		return s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"adminWarmHold": true,
			"nodeOwner":     models.GPUPoolNodeOwnerAdmin,
		})
	}
	if pool.State == models.GPUPoolStateProvisioning {
		if err := s.ensureProvisionJob(ctx, pool); err != nil {
			return nil, err
		}
		return s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"adminWarmHold": true,
			"nodeOwner":     models.GPUPoolNodeOwnerAdmin,
		})
	}

	recovery, recoverErr := s.AdminRecover(ctx, serviceTag)
	if recoverErr != nil {
		return nil, recoverErr
	}
	if recovery != nil && recovery.Recovered && recovery.Pool != nil && recovery.Pool.State == models.GPUPoolStateReady {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))
		return s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"adminWarmHold": true,
			"nodeOwner":     models.GPUPoolNodeOwnerAdmin,
		})
	}
	if recovery != nil && recovery.Action == "conflict-multiple-nodes" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "multiple reusable provider nodes found; recover or clean up before warm start")
	}

	if destroying, err := s.adminDestroyInProgress(ctx, serviceTag); err != nil {
		return nil, err
	} else if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "Resource shutdown already in progress")
	}
	if err := s.enqueueProvision(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}
	return s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":          models.GPUPoolStateProvisioning,
		"adminWarmHold":  true,
		"nodeOwner":      models.GPUPoolNodeOwnerAdmin,
		"lastError":      "",
		"lastErrorRaw":   "",
		"drainStartedAt": nil,
		"drainReason":    "",
	})
}

func (s *gpuPoolService) AdminShutdown(ctx context.Context, serviceTag string, immediate bool) (*models.GPUPool, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}

	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}

	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}

	if destroying, err := s.jobSvc.HasRunningGPUPoolDestroy(ctx, serviceTag); err != nil {
		return nil, err
	} else if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "Resource shutdown already in progress")
	}

	poolBefore, _ := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if poolBefore != nil {
		for _, sess := range poolBefore.Sessions {
			if sess.StoppedAt == nil && sess.BillingStartedAt != nil {
				_ = s.finalizeBillingOnStop(ctx, serviceTag, sess.UserID)
			}
		}
	}

	if poolBefore != nil && poolBefore.NodeID == "" && poolBefore.PublicIP == "" {
		switch poolBefore.State {
		case models.GPUPoolStateProvisioning:
			_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.provisionKey(serviceTag))
			_, _ = s.jobSvc.CancelRunningByIdempotencyKey(ctx, s.provisionKey(serviceTag))
			now := time.Now().UTC()
			updated, updateErr := s.poolRepo.Update(ctx, serviceTag, map[string]any{
				"state":          models.GPUPoolStateDraining,
				"drainReason":    models.GPUPoolDrainReasonAdminGrace,
				"drainStartedAt": now,
				"destroyAt":      nil,
				"refCount":       0,
				"adminWarmHold":  false,
			})
			if updateErr != nil {
				return nil, updateErr
			}
			if err := s.enqueueDestroy(ctx, serviceTag, serviceName); err != nil {
				return nil, err
			}
			return updated, nil
		case models.GPUPoolStateFailed, models.GPUPoolStateDraining:
			return s.adminAbortPoolNoNode(ctx, serviceTag)
		}
	}

	// Pool already in grace drain — admin override without re-running session finalize.
	if poolBefore != nil && poolBefore.State == models.GPUPoolStateDraining && poolBefore.HasScheduledGraceDestroy() {
		_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))
		if immediate {
			_, _ = s.poolRepo.Update(ctx, serviceTag, map[string]any{"adminWarmHold": false})
			if err := s.enqueueDestroy(ctx, serviceTag, serviceName); err != nil {
				return nil, err
			}
			return poolBefore, nil
		}
		now := time.Now().UTC()
		updated, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
			"drainReason":    models.GPUPoolDrainReasonAdminGrace,
			"drainStartedAt": now,
			"destroyAt":      now.Add(s.gracePeriodFor(ctx, serviceTag)),
			"refCount":       0,
			"adminWarmHold":  false,
		})
		if err != nil {
			return nil, err
		}
		if err := s.enqueueGraceDestroy(ctx, serviceTag, serviceName); err != nil {
			return nil, err
		}
		return updated, nil
	}

	pool, err := s.poolRepo.AdminShutdown(ctx, serviceTag)
	if err != nil {
		return nil, err
	}

	_, _ = s.jobSvc.CancelPendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag))

	_, _ = s.poolRepo.Update(ctx, serviceTag, map[string]any{"adminWarmHold": false})

	if immediate {
		if _, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{"destroyAt": nil}); err == nil {
			pool.DestroyAt = nil
		}
		if err := s.enqueueDestroy(ctx, serviceTag, serviceName); err != nil {
			return nil, err
		}
		return pool, nil
	}

	updated, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"drainReason": models.GPUPoolDrainReasonAdminGrace,
		"destroyAt":   time.Now().UTC().Add(s.gracePeriodFor(ctx, serviceTag)),
	})
	if err != nil {
		return nil, err
	}
	if err := s.enqueueGraceDestroy(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *gpuPoolService) AdminExtendGrace(ctx context.Context, serviceTag string, extend time.Duration) (*models.GPUPool, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}
	if extend <= 0 {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "extend duration must be positive")
	}
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}
	if pool.State != models.GPUPoolStateDraining || !pool.HasScheduledGraceDestroy() {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "pool is not in grace drain")
	}
	destroying, err := s.adminDestroyInProgress(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if destroying {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 409, "immediate destroy already in progress")
	}
	base := s.destroyAtForPool(ctx, pool)
	next := time.Now().UTC().Add(extend)
	if base != nil && base.After(time.Now().UTC()) {
		next = base.Add(extend)
	}
	if _, err := s.jobSvc.ReschedulePendingByIdempotencyKey(ctx, s.graceDestroyKey(serviceTag), next); err != nil {
		return nil, err
	}
	return s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"destroyAt":      next,
		"drainStartedAt": time.Now().UTC(),
	})
}

func (s *gpuPoolService) GetStatus(ctx context.Context, userID, serviceTag string) (*models.GPUPoolStatusResponse, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}

	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		serviceName, svcErr := s.resolveServiceName(serviceTag)
		if svcErr != nil {
			return nil, svcErr
		}
		pool, err = s.poolRepo.EnsurePool(ctx, serviceTag, serviceName)
		if err != nil {
			return nil, err
		}
	}
	reconciled, err := s.reconcilePool(ctx, pool)
	if err != nil {
		return nil, err
	}
	reconciled, err = s.reconcileIdleWarmPool(ctx, reconciled)
	if err != nil {
		return nil, err
	}
	reconciled, err = s.reconcileStuckProvision(ctx, reconciled)
	if err != nil {
		return nil, err
	}
	reconciled, err = s.reconcileUserSession(ctx, reconciled, userID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureBillingStarted(ctx, reconciled, userID); err != nil {
		return nil, err
	}
	if err := s.ensureMeterTickScheduled(ctx, reconciled, userID); err != nil {
		return nil, err
	}
	reconciled, err = s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	return s.toStatus(ctx, reconciled, userID), nil
}

func (s *gpuPoolService) ListAdmin(ctx context.Context) ([]*models.GPUPool, error) {
	pools, err := s.poolRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i, pool := range pools {
		if pool == nil {
			continue
		}
		reconciled, err := s.reconcileIdleWarmPool(ctx, pool)
		if err != nil {
			return nil, err
		}
		reconciled, err = s.reconcilePool(ctx, reconciled)
		if err != nil {
			return nil, err
		}
		pools[i] = reconciled
	}
	return pools, nil
}

func (s *gpuPoolService) AdminAbortProvision(ctx context.Context, serviceTag string) error {
	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return err
	}
	return s.abortProvisionAndDestroy(ctx, serviceTag, serviceName)
}

func (s *gpuPoolService) AdminRetryProvision(ctx context.Context, serviceTag string) error {
	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return err
	}
	active, err := s.jobSvc.HasActiveByIdempotencyKey(ctx, s.provisionKey(serviceTag))
	if err != nil {
		return err
	}
	if active {
		return apperrors.NewAppError(apperrors.ErrValidation, 409, "provision job already active")
	}
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return err
	}
	if pool == nil {
		return apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}
	if pool.State != models.GPUPoolStateFailed && pool.State != models.GPUPoolStateIdle {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "pool must be idle or failed to retry provision")
	}
	if pool.State == models.GPUPoolStateIdle && pool.LastError == "" && pool.LastErrorRaw == "" {
		return apperrors.NewAppError(apperrors.ErrValidation, 400, "pool is not in a failed state")
	}
	_, _ = s.jobSvc.MarkDeadByIdempotencyKey(ctx, s.provisionKey(serviceTag), "admin retry provision")
	if _, err := s.poolRepo.Update(ctx, serviceTag, map[string]any{
		"state":        models.GPUPoolStateIdle,
		"lastError":    "",
		"lastErrorRaw": "",
	}); err != nil {
		return err
	}
	return s.enqueueProvision(ctx, serviceTag, serviceName)
}

func (s *gpuPoolService) AdminRecover(ctx context.Context, serviceTag string) (*models.GPUPoolRecoveryReport, error) {
	if serviceTag == "" {
		return nil, apperrors.NewAppError(apperrors.ErrValidation, 400, "serviceTag is required")
	}
	serviceName, err := s.resolveServiceName(serviceTag)
	if err != nil {
		return nil, err
	}
	if _, err := s.poolRepo.EnsurePool(ctx, serviceTag, serviceName); err != nil {
		return nil, err
	}
	pool, err := s.poolRepo.GetByServiceTag(ctx, serviceTag)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, apperrors.NewAppError(apperrors.ErrNotFound, 404, "gpu pool not found")
	}

	report, err := s.probeRecoveryTruth(ctx, pool)
	if err != nil {
		return nil, err
	}
	report.Pool = pool

	update := map[string]any{
		"lastError":    "",
		"lastErrorRaw": "",
	}
	switch report.Action {
	case "adopt-ssh", "adopt-provider":
		now := time.Now().UTC()
		node := ""
		ip := ""
		if len(report.ProviderNodes) > 0 {
			node = report.ProviderNodes[0].ID
			ip = report.ProviderNodes[0].PublicIP
		}
		if node == "" && pool.NodeID != "" {
			node = pool.NodeID
		}
		if ip == "" && pool.PublicIP != "" {
			ip = pool.PublicIP
		}
		if report.ProviderNodeCount == 1 && len(report.ProviderNodes) == 1 {
			node = report.ProviderNodes[0].ID
			ip = report.ProviderNodes[0].PublicIP
		}
		if node == "" && ip == "" {
			report.Recovered = false
			report.Action = "recover-report-only"
			report.Message = "Reusable node was detected, but its details could not be synced."
			update = nil
			break
		}
		if claimed, claimErr := s.NodeClaimedByOtherPool(ctx, serviceTag, node, ip); claimErr != nil {
			return nil, claimErr
		} else if claimed != nil {
			report.Recovered = false
			report.Action = "recreate-required"
			report.Message = fmt.Sprintf("Node %s is already claimed by pool %s — not adopting onto %s.",
				strings.TrimSpace(node+" "+ip), claimed.ServiceTag, serviceTag)
			report.Notes = append(report.Notes, report.Message)
			update = nil
			break
		}
		update["state"] = models.GPUPoolStateReady
		update["readyAt"] = now
		update["nodeId"] = node
		update["publicIp"] = ip
		update["provider"] = report.Provider
		update["drainStartedAt"] = nil
		update["drainReason"] = ""
		report.Recovered = true
		if report.Action == "adopt-provider" && node == "" && ip == "" {
			report.Message = "Provider reported one reusable node, but the node details could not be parsed."
		}
	case "recreate-required":
		if keepTrackedPoolOnStaleRecreate(pool) {
			update = nil
			report.Action = "recover-report-only"
			report.Recovered = true
			report.Message = "Provider listing was stale; keeping the tracked pool state so a warm session is not torn down."
			break
		}
		update["state"] = models.GPUPoolStateIdle
		update["nodeId"] = ""
		update["publicIp"] = ""
		update["readyAt"] = nil
		update["drainStartedAt"] = nil
		update["drainReason"] = ""
		report.Recovered = false
	case "conflict-multiple-nodes":
		update = nil
		report.Recovered = false
	default:
		update = nil
	}

	if update != nil {
		updated, updErr := s.poolRepo.Update(ctx, serviceTag, update)
		if updErr != nil {
			return nil, updErr
		}
		report.Pool = updated
	}
	return report, nil
}

func keepTrackedPoolOnStaleRecreate(pool *models.GPUPool) bool {
	if pool == nil {
		return false
	}
	return pool.State == models.GPUPoolStateReady ||
		pool.State == models.GPUPoolStateProvisioning ||
		pool.NodeID != "" ||
		pool.PublicIP != ""
}

// NodeClaimedByOtherPool reports another pool that already tracks this VM.
func (s *gpuPoolService) NodeClaimedByOtherPool(ctx context.Context, serviceTag, nodeID, publicIP string) (*models.GPUPool, error) {
	nodeID = strings.TrimSpace(nodeID)
	publicIP = strings.TrimSpace(publicIP)
	if nodeID == "" && publicIP == "" {
		return nil, nil
	}
	pools, err := s.poolRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	selfCanon := models.CanonicalGPUServiceTag(serviceTag)
	for _, other := range pools {
		if other == nil || other.ServiceTag == serviceTag {
			continue
		}
		// Legacy alias of the same product may share identity — still block terminate
		// when that alias has an active session or live ready state.
		sameNode := (nodeID != "" && strings.TrimSpace(other.NodeID) == nodeID) ||
			(publicIP != "" && publicIP != "-" && strings.TrimSpace(other.PublicIP) == publicIP)
		if !sameNode {
			continue
		}
		if other.RefCount > 0 {
			return other, nil
		}
		switch other.State {
		case models.GPUPoolStateReady, models.GPUPoolStateProvisioning:
			return other, nil
		}
		// Same canonical product with a tracked node — do not steal/destroy.
		if models.CanonicalGPUServiceTag(other.ServiceTag) == selfCanon &&
			(strings.TrimSpace(other.NodeID) != "" || strings.TrimSpace(other.PublicIP) != "") {
			return other, nil
		}
	}
	return nil, nil
}
