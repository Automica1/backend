package handlers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type AdminHandler struct {
	adminService services.AdminService
}

func NewAdminHandler(adminService services.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

func (h *AdminHandler) Search(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	query := r.URL.Query().Get("q")
	response, err := h.adminService.Search(r.Context(), query)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *AdminHandler) RecentAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}

	response, err := h.adminService.GetRecentAuditLogs(r.Context(), limit)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *AdminHandler) Logs(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	query := models.AdminLogQuery{
		Source:    r.URL.Query().Get("source"),
		Kind:      r.URL.Query().Get("kind"),
		Search:    r.URL.Query().Get("search"),
		Level:     r.URL.Query().Get("level"),
		Email:     r.URL.Query().Get("email"),
		UserID:    r.URL.Query().Get("userId"),
		Route:     r.URL.Query().Get("route"),
		RequestID: r.URL.Query().Get("requestId"),
		Status:    r.URL.Query().Get("status"),
		Target:    r.URL.Query().Get("target"),
		Limit:     parseIntQuery(r, "limit", 25),
		Skip:      parseIntQuery(r, "skip", 0),
	}

	if startStr := r.URL.Query().Get("start_date"); startStr != "" {
		if parsed, err := time.Parse("2006-01-02", startStr); err == nil {
			query.StartDate = &parsed
		}
	}
	if endStr := r.URL.Query().Get("end_date"); endStr != "" {
		if parsed, err := time.Parse("2006-01-02", endStr); err == nil {
			query.EndDate = &parsed
		}
	}

	response, err := h.adminService.GetLogs(r.Context(), query)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *AdminHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	response, err := h.adminService.GetSummary(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func (h *AdminHandler) ExportSummaryCSV(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsAdminFromContext(r.Context()) {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrForbidden,
			http.StatusForbidden,
			"admin access required",
		))
		return
	}

	summary, err := h.adminService.GetSummary(r.Context())
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	rows := [][]string{
		{"Metric", "Value"},
		{"Generated At", summary.GeneratedAt.Format(time.RFC3339)},
		{"Total Users", strconv.Itoa(summary.TotalUsers)},
		{"Active Users", strconv.Itoa(summary.ActiveUsers)},
		{"Total Tokens", strconv.Itoa(summary.TotalTokens)},
		{"Used Tokens", strconv.Itoa(summary.UsedTokens)},
		{"Total Plans", strconv.Itoa(summary.TotalPlans)},
		{"Active Plans", strconv.Itoa(summary.ActivePlans)},
		{"Recent Audit Records", strconv.Itoa(summary.RecentAuditCount)},
		{"Most Used Service", summary.MostUsedService},
		{"Most Used Calls", strconv.Itoa(summary.MostUsedCalls)},
		{"Total Usage Credits", fmt.Sprintf("%d", summary.TotalUsageCredits)},
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.WriteAll(rows); err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to create csv export",
		))
		return
	}

	filename := fmt.Sprintf("admin-summary-%s.csv", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func parseIntQuery(r *http.Request, key string, defaultValue int) int {
	if value := r.URL.Query().Get(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}
