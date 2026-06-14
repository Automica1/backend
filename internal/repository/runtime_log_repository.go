package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"chi-mongo-backend/internal/models"
)

type RuntimeLogRepository interface {
	GetLogs(ctx context.Context, query models.AdminLogQuery) ([]models.AdminLogEntry, int64, error)
}

type runtimeLogRepository struct {
	backendAccessPath string
	backendErrorPath  string
	nginxAccessPath   string
	nginxErrorPath    string
}

func NewRuntimeLogRepository(accessPath, errorPath, nginxAccessPath, nginxErrorPath string) RuntimeLogRepository {
	return &runtimeLogRepository{
		backendAccessPath: accessPath,
		backendErrorPath:  errorPath,
		nginxAccessPath:   nginxAccessPath,
		nginxErrorPath:    nginxErrorPath,
	}
}

func (r *runtimeLogRepository) GetLogs(ctx context.Context, query models.AdminLogQuery) ([]models.AdminLogEntry, int64, error) {
	_ = ctx

	files := r.filesForSource(query.Source)
	entries := make([]models.AdminLogEntry, 0, 256)

	for _, file := range files {
		fileEntries, err := r.readFile(file.path, file.fileKind, query.Source)
		if err != nil {
			if os.IsNotExist(err) || errors.Is(err, os.ErrPermission) {
				continue
			}
			return nil, 0, err
		}
		entries = append(entries, fileEntries...)
	}

	filtered := make([]models.AdminLogEntry, 0, len(entries))
	for _, entry := range entries {
		if matchesLogQuery(entry, query) {
			filtered = append(filtered, entry)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Timestamp.Equal(filtered[j].Timestamp) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})

	total := int64(len(filtered))
	start := query.Skip
	if start < 0 {
		start = 0
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + query.Limit
	if query.Limit <= 0 {
		end = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	paged := filtered[start:end]
	return paged, total, nil
}

func (r *runtimeLogRepository) filesForSource(source string) []struct {
	path     string
	fileKind string
} {
	switch source {
	case "backend-access":
		return []struct {
			path     string
			fileKind string
		}{{path: r.backendAccessPath, fileKind: "backend-access"}}
	case "backend-error":
		return []struct {
			path     string
			fileKind string
		}{{path: r.backendErrorPath, fileKind: "backend-error"}}
	case "web-access":
		return []struct {
			path     string
			fileKind string
		}{{path: r.nginxAccessPath, fileKind: "web-access"}}
	case "web-error":
		return []struct {
			path     string
			fileKind string
		}{{path: r.nginxErrorPath, fileKind: "web-error"}}
	default:
		return []struct {
			path     string
			fileKind string
		}{
			{path: r.backendAccessPath, fileKind: "backend-access"},
			{path: r.backendErrorPath, fileKind: "backend-error"},
			{path: r.nginxAccessPath, fileKind: "web-access"},
			{path: r.nginxErrorPath, fileKind: "web-error"},
		}
	}
}

func (r *runtimeLogRepository) readFile(path string, fileKind string, source string) ([]models.AdminLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	entries := make([]models.AdminLogEntry, 0, 128)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		entry, ok := parseRuntimeLogLine(line, filepath.Base(path), stat.ModTime(), fileKind, source)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func parseRuntimeLogLine(line, fileName string, fallback time.Time, fileKind string, source string) (models.AdminLogEntry, bool) {
	entry := models.AdminLogEntry{
		ID:        fmt.Sprintf("%s-%d", fileName, fallback.UnixNano()),
		Source:    "runtime",
		Timestamp: fallback,
	}

	switch fileKind {
	case "web-access":
		return parseNginxAccessLine(line, fileName, fallback, source)
	case "web-error":
		return parseNginxErrorLine(line, fileName, fallback, source)
	}

	jsonStart := strings.IndexByte(line, '{')
	if jsonStart == -1 {
		if fileKind != "backend-error" || !looksLikeErrorLine(line) {
			return models.AdminLogEntry{}, false
		}
		entry.Category = "backend-error"
		entry.Source = "backend-runtime"
		entry.Level = "error"
		entry.Message = line
		entry.ID = fmt.Sprintf("%s-%d", fileName, time.Now().UnixNano())
		return entry, true
	}

	if prefix := strings.TrimSpace(line[:jsonStart]); prefix != "" {
		if parsed, err := time.Parse("2006/01/02 15:04:05", prefix); err == nil {
			entry.Timestamp = parsed
		}
	}

	rawJSON := strings.TrimSpace(line[jsonStart:])
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		if fileKind != "backend-error" || !looksLikeErrorLine(line) {
			return models.AdminLogEntry{}, false
		}
		entry.Category = "backend-error"
		entry.Source = "backend-runtime"
		entry.Level = "error"
		entry.Message = line
		entry.ID = fmt.Sprintf("%s-%d", fileName, time.Now().UnixNano())
		return entry, true
	}

	entry.ID = buildEntryID(fileName, payload, entry.Timestamp)
	entry.Message = stringValueFromKeys(payload, "message")
	if ts := stringValue(payload["timestamp"]); ts != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			entry.Timestamp = parsed
		}
	}
	entry.Level = stringValueFromKeys(payload, "level")
	entry.RequestID = stringValueFromKeys(payload, "request_id", "requestId")
	entry.Method = stringValueFromKeys(payload, "method")
	entry.Path = stringValueFromKeys(payload, "path")
	entry.Route = stringValueFromKeys(payload, "route")
	entry.RemoteIP = stringValueFromKeys(payload, "remote_ip", "remoteIp")
	entry.UserAgent = stringValueFromKeys(payload, "user_agent", "userAgent")
	entry.Email = stringValueFromKeys(payload, "email")
	entry.ActorEmail = stringValueFromKeys(payload, "actorEmail")
	entry.ActorID = stringValueFromKeys(payload, "actorId")
	entry.Action = stringValueFromKeys(payload, "action")
	entry.TargetType = stringValueFromKeys(payload, "targetType")
	entry.TargetID = stringValueFromKeys(payload, "targetId")
	entry.Outcome = stringValueFromKeys(payload, "outcome")
	entry.Reason = stringValueFromKeys(payload, "reason")
	entry.UserID = stringValueFromKeys(payload, "userId")
	entry.ServiceName = stringValueFromKeys(payload, "serviceName", "service_name")
	entry.Endpoint = stringValueFromKeys(payload, "endpoint")
	entry.AuthMethod = stringValueFromKeys(payload, "authMethod", "auth_method")
	entry.IPAddress = stringValueFromKeys(payload, "ipAddress", "ip_address")
	entry.Metadata = payload

	entry.Status = intValueFromKeys(payload, "status")
	entry.Bytes = intValueFromKeys(payload, "bytes")
	entry.CreditsUsed = intValueFromKeys(payload, "creditsUsed", "credits_used")
	entry.DurationMS = int64ValueFromKeys(payload, "durationMs", "duration_ms")
	entry.ProcessTimeMS = int64ValueFromKeys(payload, "processTimeMs", "process_time_ms")
	entry.IsAdmin = boolValueFromKeys(payload, "is_admin", "isAdmin")
	entry.Success = boolValueFromKeys(payload, "success")
	entry.Kind = classifyRuntimeKind(entry.Path, entry.Route, entry.Message, fileKind, source, true)

	if isAccessPayload(payload) {
		entry.Category = "backend-access"
		entry.Source = "backend-runtime"
		if entry.Message == "" {
			entry.Message = "http_request"
		}
		return entry, fileKind != "backend-error"
	}

	if fileKind == "backend-access" {
		return models.AdminLogEntry{}, false
	}

	if !looksLikeErrorEntry(payload, line) && source != "all" {
		return models.AdminLogEntry{}, false
	}

	entry.Category = "backend-error"
	entry.Source = "backend-runtime"
	entry.Kind = "error"
	if entry.Message == "" {
		entry.Message = line
	}
	if entry.Level == "" {
		entry.Level = "error"
	}
	return entry, true
}

var nginxAccessPatternFull = regexp.MustCompile(`^(\S+) - - \[([^\]]+)\] "([A-Z]+) (.+?) HTTP/[^"]+" (\d{3}) (\S+) "([^"]*)" "([^"]*)" "([^"]*)" "([^"]*)"$`)
var nginxAccessPatternCombined = regexp.MustCompile(`^(\S+) - - \[([^\]]+)\] "([A-Z]+) (.+?) HTTP/[^"]+" (\d{3}) (\S+) "([^"]*)" "([^"]*)"$`)
var nginxAccessPatternUnquoted = regexp.MustCompile(`^(\S+) - - \[([^\]]+)\] ([A-Z]+) (.+?) HTTP/[^ ]+ (\d{3}) (\S+) (\S+) (.+?) (\S+) (\S+)$`)
var nginxErrorPattern = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[(\w+)\] ([^:]+):(?: \*(\d+))? (.*?)(?:, client: ([^,]+))?(?:, server: ([^,]+))?(?:, request: "([^"]+)")?(?:, host: "([^"]+)")?(?:, referrer: "([^"]+)")?$`)

func parseNginxAccessLine(line, fileName string, fallback time.Time, source string) (models.AdminLogEntry, bool) {
	matches := nginxAccessPatternFull.FindStringSubmatch(line)
	format := "full"
	if len(matches) == 0 {
		matches = nginxAccessPatternCombined.FindStringSubmatch(line)
		format = "combined"
	}
	if len(matches) == 0 {
		matches = nginxAccessPatternUnquoted.FindStringSubmatch(line)
		format = "unquoted"
	}
	if len(matches) == 0 {
		return models.AdminLogEntry{}, false
	}

	parsedAt, err := time.Parse("02/Jan/2006:15:04:05 -0700", matches[2])
	if err != nil {
		parsedAt = fallback
	}

	status, _ := strconv.Atoi(matches[5])
	bytesSent := 0
	if matches[6] != "-" {
		bytesSent, _ = strconv.Atoi(matches[6])
	}

	requestPath := matches[4]
	if parsedURL, err := url.Parse(requestPath); err == nil {
		requestPath = parsedURL.RequestURI()
	}

	entry := models.AdminLogEntry{
		ID:        fmt.Sprintf("%s-%s", fileName, matches[2]),
		Category:  "web-access",
		Kind:      classifyRuntimeKind(requestPath, requestPath, line, fileName, source, false),
		Source:    "nginx",
		Timestamp: parsedAt,
		Level:     "info",
		Message:   fmt.Sprintf("%s %s", matches[3], requestPath),
		RemoteIP:  matches[1],
		Referer:   matches[7],
		UserAgent: matches[8],
		Method:    matches[3],
		Path:      requestPath,
		Route:     requestPath,
		Status:    status,
		Bytes:     bytesSent,
		Metadata: map[string]interface{}{
			"raw":       line,
			"server":    fileName,
			"file_kind": "web-access",
			"format":    format,
		},
	}
	if format == "full" {
		entry.ClientIP = matches[1]
		entry.Host = matches[9]
		entry.Upstream = matches[10]
	} else {
		entry.ClientIP = matches[1]
		if format == "unquoted" {
			entry.Referer = matches[7]
			entry.UserAgent = matches[8]
			entry.Host = matches[9]
			entry.Upstream = matches[10]
		}
	}
	if entry.Message == "" {
		entry.Message = "nginx_request"
	}
	if source == "all" || source == "web-access" {
		return entry, true
	}
	return entry, false
}

func parseNginxErrorLine(line, fileName string, fallback time.Time, source string) (models.AdminLogEntry, bool) {
	matches := nginxErrorPattern.FindStringSubmatch(line)
	if len(matches) == 0 {
		return models.AdminLogEntry{}, false
	}

	parsedAt, err := time.Parse("2006/01/02 15:04:05", matches[1])
	if err != nil {
		parsedAt = fallback
	}

	level := strings.ToLower(matches[2])
	requestLine := strings.TrimSpace(matches[8])
	requestPath := ""
	method := ""
	if requestLine != "" {
		parts := strings.SplitN(requestLine, " ", 3)
		if len(parts) >= 2 {
			method = parts[0]
			requestPath = parts[1]
		}
	}

	entry := models.AdminLogEntry{
		ID:        fmt.Sprintf("%s-%s", fileName, matches[1]),
		Category:  "web-error",
		Kind:      "error",
		Source:    "nginx",
		Timestamp: parsedAt,
		Level:     level,
		Message:   matches[5],
		ClientIP:  matches[6],
		Host:      matches[9],
		Referer:   matches[10],
		Method:    method,
		Path:      requestPath,
		Route:     requestPath,
		Metadata: map[string]interface{}{
			"raw":       line,
			"server":    matches[7],
			"request":   requestLine,
			"file_kind": "web-error",
		},
	}
	if source == "all" || source == "web-error" {
		return entry, true
	}
	return entry, false
}

func matchesLogQuery(entry models.AdminLogEntry, query models.AdminLogQuery) bool {
	if query.Source != "" && query.Source != "all" && entry.Category != query.Source {
		return false
	}
	if query.Level != "" && !strings.EqualFold(entry.Level, query.Level) {
		return false
	}
	if query.Kind != "" && !matchesLogKind(entry.Kind, query.Kind) {
		return false
	}
	if query.Email != "" && !containsFold(entry.Email, query.Email) {
		return false
	}
	if query.UserID != "" && !containsFold(entry.UserID, query.UserID) {
		return false
	}
	if query.Route != "" && !containsFold(entry.Route, query.Route) && !containsFold(entry.Path, query.Route) {
		return false
	}
	if query.RequestID != "" && !containsFold(entry.RequestID, query.RequestID) {
		return false
	}
	if query.Status != "" && strconv.Itoa(entry.Status) != query.Status {
		return false
	}
	if query.Target != "" && !containsFold(entry.TargetType, query.Target) && !containsFold(entry.TargetID, query.Target) {
		return false
	}
	if query.Search != "" {
		needle := query.Search
		if !containsFold(entry.Message, needle) &&
			!containsFold(entry.Path, needle) &&
			!containsFold(entry.Route, needle) &&
			!containsFold(entry.Email, needle) &&
			!containsFold(entry.UserID, needle) &&
			!containsFold(entry.RequestID, needle) &&
			!containsFold(entry.ActorEmail, needle) &&
			!containsFold(entry.Action, needle) &&
			!containsFold(entry.TargetType, needle) &&
			!containsFold(entry.TargetID, needle) &&
			!containsFold(entry.ServiceName, needle) &&
			!containsFold(entry.ClientIP, needle) &&
			!containsFold(entry.Referer, needle) &&
			!containsFold(entry.Host, needle) {
			return false
		}
	}
	if query.StartDate != nil && entry.Timestamp.Before(query.StartDate.Add(-time.Nanosecond)) {
		return false
	}
	if query.EndDate != nil && entry.Timestamp.After(query.EndDate.Add(24*time.Hour)) {
		return false
	}
	return true
}

func isAccessPayload(payload map[string]any) bool {
	if message := stringValue(payload["message"]); message == "http_request" {
		return true
	}
	return stringValue(payload["method"]) != "" && stringValue(payload["path"]) != ""
}

func looksLikeErrorEntry(payload map[string]any, line string) bool {
	if level := strings.ToLower(stringValue(payload["level"])); level == "error" || level == "fatal" || level == "warn" {
		return true
	}
	if message := strings.ToLower(stringValue(payload["message"])); message != "" {
		return strings.Contains(message, "error") || strings.Contains(message, "failed") || strings.Contains(message, "panic") || strings.Contains(message, "timeout")
	}
	lowerLine := strings.ToLower(line)
	return strings.Contains(lowerLine, "error") || strings.Contains(lowerLine, "failed") || strings.Contains(lowerLine, "panic") || strings.Contains(lowerLine, "timeout")
}

func looksLikeErrorLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "panic") || strings.Contains(lower, "timeout")
}

func classifyRuntimeKind(path, route, message, fileKind, source string, backend bool) string {
	candidate := strings.TrimSpace(path)
	if candidate == "" {
		candidate = strings.TrimSpace(route)
	}
	if candidate == "" {
		candidate = strings.TrimSpace(message)
	}
	candidate = strings.ToLower(candidate)

	if backend {
		if candidate == "" {
			return "api-request"
		}
		if isInternalRequestPath(candidate) {
			return "internal-request"
		}
		if strings.Contains(candidate, "/api/") || strings.Contains(candidate, "/go/") {
			return "api-request"
		}
		if strings.HasPrefix(candidate, "/admin/") {
			return "page-view"
		}
		return "api-request"
	}

	if fileKind == "web-error" {
		return "error"
	}

	if candidate == "" {
		return "page-view"
	}
	if strings.Contains(candidate, "_rsc=") {
		return "page-view"
	}
	if isInternalRequestPath(candidate) {
		return "internal-request"
	}
	if strings.HasPrefix(candidate, "/api/") || strings.HasPrefix(candidate, "/go/api/") {
		return "api-request"
	}
	if strings.HasPrefix(candidate, "/_next/") || strings.Contains(candidate, "_next/image") {
		return "asset-request"
	}
	if hasStaticAssetSuffix(candidate) {
		return "asset-request"
	}
	if strings.Contains(candidate, "logo.png") || strings.Contains(candidate, "og-image") || strings.Contains(candidate, "favicon") {
		return "asset-request"
	}
	return "page-view"
}

func isInternalRequestPath(candidate string) bool {
	return strings.Contains(candidate, "/api/auth/setup") ||
		strings.HasPrefix(candidate, "/api/auth") ||
		strings.Contains(candidate, "/api/auth/")
}

func hasStaticAssetSuffix(candidate string) bool {
	for _, suffix := range []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".map", ".woff", ".woff2", ".ttf", ".otf"} {
		if strings.HasSuffix(candidate, suffix) {
			return true
		}
	}
	return false
}

func matchesLogKind(entryKind, queryKind string) bool {
	if queryKind == "" || queryKind == "all" {
		return true
	}
	if queryKind == "signal" {
		switch entryKind {
		case "asset-request", "internal-request":
			return false
		case "":
			return true
		default:
			return true
		}
	}
	return strings.EqualFold(entryKind, queryKind)
}

func buildEntryID(fileName string, payload map[string]any, fallback time.Time) string {
	if id := stringValue(payload["request_id"]); id != "" {
		return fmt.Sprintf("%s-%s", fileName, id)
	}
	if ts := stringValue(payload["timestamp"]); ts != "" {
		return fmt.Sprintf("%s-%s", fileName, ts)
	}
	return fmt.Sprintf("%s-%d", fileName, fallback.UnixNano())
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func stringValueFromKeys(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(payload[key]); value != "" {
			return value
		}
	}
	return ""
}

func intValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

func intValueFromKeys(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := intValue(payload[key]); value != 0 {
			return value
		}
	}
	return 0
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return parsed
		}
	}
	return 0
}

func int64ValueFromKeys(payload map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := int64Value(payload[key]); value != 0 {
			return value
		}
	}
	return 0
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}

func boolValueFromKeys(payload map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value := boolValue(payload[key]); value {
			return true
		}
	}
	return false
}

func containsFold(value, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}
