package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/repository"
)

type AdminService interface {
	RecordAction(ctx context.Context, log *models.AdminAuditLog) error
	GetRecentAuditLogs(ctx context.Context, limit int) (*models.AdminAuditLogListResponse, error)
	GetLogs(ctx context.Context, query models.AdminLogQuery) (*models.AdminLogListResponse, error)
	Search(ctx context.Context, query string) (*models.AdminSearchResponse, error)
	GetSummary(ctx context.Context) (*models.AdminSummaryResponse, error)
}

type adminService struct {
	auditRepo    repository.AdminAuditRepository
	runtimeLogs  repository.RuntimeLogRepository
	userService  UserService
	tokenService CreditTokenService
	planService  PlanService
	usageService UsageService
	subService   SubscriptionService
}

func NewAdminService(
	auditRepo repository.AdminAuditRepository,
	runtimeLogs repository.RuntimeLogRepository,
	userService UserService,
	tokenService CreditTokenService,
	planService PlanService,
	usageService UsageService,
	subService SubscriptionService,
) AdminService {
	return &adminService{
		auditRepo:    auditRepo,
		runtimeLogs:  runtimeLogs,
		userService:  userService,
		tokenService: tokenService,
		planService:  planService,
		usageService: usageService,
		subService:   subService,
	}
}

func (s *adminService) RecordAction(ctx context.Context, logEntry *models.AdminAuditLog) error {
	if logEntry.Timestamp.IsZero() {
		logEntry.Timestamp = time.Now()
	}
	return s.auditRepo.Create(ctx, logEntry)
}

func (s *adminService) GetRecentAuditLogs(ctx context.Context, limit int) (*models.AdminAuditLogListResponse, error) {
	logs, err := s.auditRepo.GetRecent(ctx, limit)
	if err != nil {
		return nil, err
	}
	return &models.AdminAuditLogListResponse{
		Message: "Audit logs retrieved successfully",
		Logs:    logs,
		Count:   len(logs),
	}, nil
}

func (s *adminService) GetLogs(ctx context.Context, query models.AdminLogQuery) (*models.AdminLogListResponse, error) {
	source := normalizeLogSource(query.Source)
	query.Source = source
	query.Kind = normalizeLogKind(query.Kind)
	if query.Limit <= 0 {
		query.Limit = 25
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.Skip < 0 {
		query.Skip = 0
	}

	switch source {
	case "backend-access", "backend-error", "web-access", "web-error", "all":
		logs, total, err := s.runtimeLogs.GetLogs(ctx, query)
		if err != nil {
			return nil, err
		}
		entries := logs
		if source == "all" {
			entries = filterRuntimeLogs(entries, query)
		}
		return &models.AdminLogListResponse{
			Message: fmt.Sprintf("%s logs retrieved successfully", source),
			Source:  source,
			Logs:    entries,
			Total:   total,
			Limit:   query.Limit,
			Skip:    query.Skip,
		}, nil
	case "audit":
		count, err := s.auditRepo.Count(ctx)
		if err != nil {
			return nil, err
		}
		logs, err := s.auditRepo.GetRecent(ctx, int(count))
		if err != nil {
			return nil, err
		}
		entries := filterAuditLogs(mapAuditLogs(logs), query)
		total := int64(len(entries))
		entries = paginateEntries(entries, query.Skip, query.Limit)
		return &models.AdminLogListResponse{
			Message: "audit logs retrieved successfully",
			Source:  source,
			Logs:    entries,
			Total:   total,
			Limit:   query.Limit,
			Skip:    query.Skip,
		}, nil
	case "usage":
		count, err := s.usageService.CountUsageHistory(ctx, query.StartDate, query.EndDate)
		if err != nil {
			return nil, err
		}
		logs, err := s.usageService.GetAllUsageHistory(ctx, query.StartDate, query.EndDate, int(count), 0)
		if err != nil {
			return nil, err
		}
		entries := filterUsageLogs(mapUsageLogs(logs), query)
		total := int64(len(entries))
		entries = paginateEntries(entries, query.Skip, query.Limit)
		return &models.AdminLogListResponse{
			Message: "usage logs retrieved successfully",
			Source:  source,
			Logs:    entries,
			Total:   total,
			Limit:   query.Limit,
			Skip:    query.Skip,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported log source: %s", query.Source)
	}
}

func filterRuntimeLogs(entries []models.AdminLogEntry, query models.AdminLogQuery) []models.AdminLogEntry {
	filtered := make([]models.AdminLogEntry, 0, len(entries))
	for _, entry := range entries {
		if !matchesLogKind(entry.Kind, query.Kind) {
			continue
		}
		if query.Search != "" &&
			!strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Path), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Route), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Email), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.UserID), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.RequestID), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.ClientIP), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Referer), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Host), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.ServiceName), strings.ToLower(query.Search)) {
			continue
		}
		if query.Email != "" && !strings.Contains(strings.ToLower(entry.Email), strings.ToLower(query.Email)) {
			continue
		}
		if query.UserID != "" && !strings.Contains(strings.ToLower(entry.UserID), strings.ToLower(query.UserID)) {
			continue
		}
		if query.Route != "" && !strings.Contains(strings.ToLower(entry.Route), strings.ToLower(query.Route)) && !strings.Contains(strings.ToLower(entry.Path), strings.ToLower(query.Route)) {
			continue
		}
		if query.RequestID != "" && !strings.Contains(strings.ToLower(entry.RequestID), strings.ToLower(query.RequestID)) {
			continue
		}
		if query.Status != "" && strconv.Itoa(entry.Status) != query.Status {
			continue
		}
		if query.Level != "" && !strings.EqualFold(entry.Level, query.Level) {
			continue
		}
		if query.Target != "" && !strings.Contains(strings.ToLower(entry.TargetType), strings.ToLower(query.Target)) && !strings.Contains(strings.ToLower(entry.TargetID), strings.ToLower(query.Target)) {
			continue
		}
		if query.StartDate != nil && entry.Timestamp.Before(*query.StartDate) {
			continue
		}
		if query.EndDate != nil && entry.Timestamp.After(query.EndDate.Add(24*time.Hour)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func normalizeLogKind(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "all", "signal", "page-view", "api-request", "internal-request", "asset-request", "admin-action", "usage", "audit", "error":
		return normalized
	default:
		return ""
	}
}

func matchesLogKind(entryKind, queryKind string) bool {
	if queryKind == "" || queryKind == "all" {
		return true
	}
	if queryKind == "signal" {
		switch entryKind {
		case "asset-request", "internal-request":
			return false
		default:
			return true
		}
	}
	return strings.EqualFold(entryKind, queryKind)
}

func (s *adminService) Search(ctx context.Context, query string) (*models.AdminSearchResponse, error) {
	needle := strings.ToLower(strings.TrimSpace(query))

	usersResp, err := s.userService.GetAllUsers(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := s.tokenService.GetAllTokens(ctx)
	if err != nil {
		return nil, err
	}
	plans, err := s.planService.GetAllPlans(ctx)
	if err != nil {
		return nil, err
	}
	usageStats, err := s.usageService.GetGlobalStats(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	subscriptions, err := s.subService.GetAllSubscriptions(ctx)
	if err != nil {
		return nil, err
	}

	filterUsers := make([]models.AdminUser, 0)
	for _, user := range usersResp.Users {
		if needle == "" || strings.Contains(strings.ToLower(user.Email), needle) || strings.Contains(strings.ToLower(user.UserID), needle) {
			filterUsers = append(filterUsers, user)
		}
	}

	filterTokens := make([]*models.CreditToken, 0)
	for _, token := range tokens {
		if needle == "" ||
			strings.Contains(strings.ToLower(token.Token), needle) ||
			strings.Contains(strings.ToLower(token.Description), needle) ||
			strings.Contains(strings.ToLower(token.CreatedBy), needle) {
			filterTokens = append(filterTokens, token)
		}
	}

	filterPlans := make([]models.Plan, 0)
	for _, plan := range plans {
		if needle == "" ||
			strings.Contains(strings.ToLower(plan.Name), needle) ||
			strings.Contains(strings.ToLower(plan.PlanID), needle) ||
			strings.Contains(strings.ToLower(plan.Description), needle) {
			filterPlans = append(filterPlans, plan)
		}
	}

	filterUsage := make([]models.UsageStats, 0)
	for _, stat := range usageStats {
		if needle == "" || strings.Contains(strings.ToLower(stat.ServiceName), needle) {
			filterUsage = append(filterUsage, stat)
		}
	}

	filterSubscriptions := make([]models.AdminSubscription, 0)
	for _, sub := range subscriptions {
		item := models.AdminSubscription{
			ID:                 sub.ID,
			UserID:             sub.UserID,
			Email:              sub.Email,
			PlanID:             sub.PlanID,
			SubscriptionID:     sub.SubscriptionID,
			Status:             sub.Status,
			Amount:             sub.Amount,
			Currency:           sub.Currency,
			CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd:   sub.CurrentPeriodEnd,
			GracePeriodEnd:     sub.GracePeriodEnd,
			CancelAtCycleEnd:   sub.CancelAtCycleEnd,
			CancelScheduledAt:  sub.CancelScheduledAt,
			CancelledAt:        sub.CancelledAt,
			PendingPlanID:      sub.PendingPlanID,
			PlanChangeDate:     sub.PlanChangeDate,
			CreatedAt:          sub.CreatedAt,
			UpdatedAt:          sub.UpdatedAt,
		}
		if needle == "" ||
			strings.Contains(strings.ToLower(item.UserID), needle) ||
			strings.Contains(strings.ToLower(item.Email), needle) ||
			strings.Contains(strings.ToLower(item.PlanID), needle) ||
			strings.Contains(strings.ToLower(item.SubscriptionID), needle) ||
			strings.Contains(strings.ToLower(string(item.Status)), needle) {
			filterSubscriptions = append(filterSubscriptions, item)
		}
	}

	return &models.AdminSearchResponse{
		Message:       "Search completed successfully",
		Query:         query,
		Users:         filterUsers,
		Tokens:        filterTokens,
		Plans:         filterPlans,
		Usage:         filterUsage,
		Subscriptions: filterSubscriptions,
	}, nil
}

func (s *adminService) GetSummary(ctx context.Context) (*models.AdminSummaryResponse, error) {
	usersResp, err := s.userService.GetAllUsers(ctx)
	if err != nil {
		return nil, err
	}
	tokens, err := s.tokenService.GetAllTokens(ctx)
	if err != nil {
		return nil, err
	}
	plans, err := s.planService.GetAllPlans(ctx)
	if err != nil {
		return nil, err
	}
	usageStats, err := s.usageService.GetGlobalStats(ctx, nil, nil)
	if err != nil {
		return nil, err
	}
	recentLogs, err := s.auditRepo.GetRecent(ctx, 20)
	if err != nil {
		return nil, err
	}

	activeUsers := 0
	for _, user := range usersResp.Users {
		if user.IsActive {
			activeUsers++
		}
	}

	usedTokens := 0
	for _, token := range tokens {
		if token.IsUsed {
			usedTokens++
		}
	}

	activePlans := 0
	for _, plan := range plans {
		if plan.IsActive {
			activePlans++
		}
	}

	totalUsageCredits := 0
	mostUsedService := ""
	mostUsedCalls := 0
	for _, stat := range usageStats {
		totalUsageCredits += stat.TotalCredits
		if stat.TotalCalls > mostUsedCalls {
			mostUsedCalls = stat.TotalCalls
			mostUsedService = stat.ServiceName
		}
	}

	return &models.AdminSummaryResponse{
		Message:           "Summary retrieved successfully",
		GeneratedAt:       time.Now(),
		TotalUsers:        usersResp.Total,
		ActiveUsers:       activeUsers,
		TotalTokens:       len(tokens),
		UsedTokens:        usedTokens,
		TotalPlans:        len(plans),
		ActivePlans:       activePlans,
		RecentAuditCount:  len(recentLogs),
		MostUsedService:   mostUsedService,
		MostUsedCalls:     mostUsedCalls,
		TotalUsageCredits: int64(totalUsageCredits),
	}, nil
}

func normalizeLogSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "access", "backend-access":
		if strings.TrimSpace(source) == "" {
			return "backend-access"
		}
		return "backend-access"
	case "error", "backend-error":
		return "backend-error"
	case "web-access":
		return "web-access"
	case "web-error":
		return "web-error"
	case "audit", "usage", "all":
		return strings.ToLower(strings.TrimSpace(source))
	default:
		return "backend-access"
	}
}

func mapAuditLogs(logs []models.AdminAuditLog) []models.AdminLogEntry {
	entries := make([]models.AdminLogEntry, 0, len(logs))
	for _, logEntry := range logs {
		entries = append(entries, models.AdminLogEntry{
			ID:         logEntry.ID.Hex(),
			Category:   "audit",
			Source:     "mongo",
			Timestamp:  logEntry.Timestamp,
			Level:      "info",
			Message:    logEntry.Action,
			ActorEmail: logEntry.ActorEmail,
			ActorID:    logEntry.ActorID,
			Action:     logEntry.Action,
			TargetType: logEntry.TargetType,
			TargetID:   logEntry.TargetID,
			Outcome:    logEntry.Outcome,
			Reason:     logEntry.Reason,
			Metadata:   logEntry.Metadata,
		})
	}
	return entries
}

func mapUsageLogs(logs []models.ServiceUsage) []models.AdminLogEntry {
	entries := make([]models.AdminLogEntry, 0, len(logs))
	for _, usage := range logs {
		message := usage.ServiceName
		if usage.Endpoint != "" {
			message = usage.Endpoint
		}
		entries = append(entries, models.AdminLogEntry{
			ID:            usage.ID.Hex(),
			Category:      "usage",
			Source:        "mongo",
			Timestamp:     usage.CreatedAt,
			Level:         map[bool]string{true: "info", false: "error"}[usage.Success],
			Message:       message,
			UserID:        usage.UserID,
			Email:         usage.Email,
			ServiceName:   usage.ServiceName,
			Endpoint:      usage.Endpoint,
			AuthMethod:    usage.AuthMethod,
			CreditsUsed:   usage.CreditsUsed,
			Success:       usage.Success,
			ProcessTimeMS: usage.ProcessTime,
			RequestID:     usage.RequestID,
			IPAddress:     usage.IPAddress,
			UserAgent:     usage.UserAgent,
		})
	}
	return entries
}

func filterAuditLogs(entries []models.AdminLogEntry, query models.AdminLogQuery) []models.AdminLogEntry {
	filtered := make([]models.AdminLogEntry, 0, len(entries))
	for _, entry := range entries {
		if query.Search != "" &&
			!strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Action), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.ActorEmail), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.TargetType), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.TargetID), strings.ToLower(query.Search)) {
			continue
		}
		if query.Email != "" && !strings.Contains(strings.ToLower(entry.ActorEmail), strings.ToLower(query.Email)) {
			continue
		}
		if query.UserID != "" && !strings.Contains(strings.ToLower(entry.ActorID), strings.ToLower(query.UserID)) && !strings.Contains(strings.ToLower(entry.TargetID), strings.ToLower(query.UserID)) {
			continue
		}
		if query.Route != "" && !strings.Contains(strings.ToLower(entry.TargetType), strings.ToLower(query.Route)) && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query.Route)) {
			continue
		}
		if query.StartDate != nil && entry.Timestamp.Before(*query.StartDate) {
			continue
		}
		if query.EndDate != nil && entry.Timestamp.After(query.EndDate.Add(24*time.Hour)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func filterUsageLogs(entries []models.AdminLogEntry, query models.AdminLogQuery) []models.AdminLogEntry {
	filtered := make([]models.AdminLogEntry, 0, len(entries))
	for _, entry := range entries {
		if query.Search != "" &&
			!strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.ServiceName), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Email), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.UserID), strings.ToLower(query.Search)) &&
			!strings.Contains(strings.ToLower(entry.Endpoint), strings.ToLower(query.Search)) {
			continue
		}
		if query.Email != "" && !strings.Contains(strings.ToLower(entry.Email), strings.ToLower(query.Email)) {
			continue
		}
		if query.UserID != "" && !strings.Contains(strings.ToLower(entry.UserID), strings.ToLower(query.UserID)) {
			continue
		}
		if query.Route != "" && !strings.Contains(strings.ToLower(entry.Endpoint), strings.ToLower(query.Route)) && !strings.Contains(strings.ToLower(entry.ServiceName), strings.ToLower(query.Route)) {
			continue
		}
		if query.Status != "" {
			wantSuccess := strings.EqualFold(query.Status, "success") || query.Status == "1"
			if entry.Success != wantSuccess {
				continue
			}
		}
		if query.StartDate != nil && entry.Timestamp.Before(*query.StartDate) {
			continue
		}
		if query.EndDate != nil && entry.Timestamp.After(query.EndDate.Add(24*time.Hour)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func paginateEntries(entries []models.AdminLogEntry, skip, limit int) []models.AdminLogEntry {
	if skip < 0 {
		skip = 0
	}
	if limit <= 0 {
		limit = len(entries)
	}
	if skip > len(entries) {
		return []models.AdminLogEntry{}
	}
	end := skip + limit
	if end > len(entries) {
		end = len(entries)
	}
	return entries[skip:end]
}
