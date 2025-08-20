// internal/handlers/usage.go
package handlers

import (
	"net/http"
	"strconv"
	"time"

	"chi-mongo-backend/internal/services"
	"chi-mongo-backend/pkg/utils"
	apperrors "chi-mongo-backend/pkg/errors"
	"github.com/go-chi/chi/v5"
)

type UsageHandler struct {
	usageService services.UsageService
}

func NewUsageHandler(usageService services.UsageService) *UsageHandler {
	return &UsageHandler{
		usageService: usageService,
	}
}

func (h *UsageHandler) GetGlobalStats(w http.ResponseWriter, r *http.Request) {
	startDate, endDate := h.parseDateRange(r)
	
	stats, err := h.usageService.GetGlobalStats(r.Context(), startDate, endDate)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to get global usage stats: "+err.Error(),
		))
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"stats": stats,
		"total_services": len(stats),
		"date_range": map[string]interface{}{
			"start_date": startDate,
			"end_date": endDate,
		},
	})
}

func (h *UsageHandler) GetUserStats(w http.ResponseWriter, r *http.Request) {
	startDate, endDate := h.parseDateRange(r)
	
	stats, err := h.usageService.GetUserStats(r.Context(), startDate, endDate)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to get user usage stats: "+err.Error(),
		))
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"stats": stats,
		"total_users": len(stats),
		"date_range": map[string]interface{}{
			"start_date": startDate,
			"end_date": endDate,
		},
	})
}

func (h *UsageHandler) GetServiceUserStats(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	startDate, endDate := h.parseDateRange(r)
	
	stats, err := h.usageService.GetServiceUserStats(r.Context(), serviceName, startDate, endDate)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to get service user stats: "+err.Error(),
		))
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"stats": stats,
		"service": serviceName,
		"total_records": len(stats),
		"date_range": map[string]interface{}{
			"start_date": startDate,
			"end_date": endDate,
		},
	})
}

// NEW: GetUserUsageHistory method
func (h *UsageHandler) GetUserUsageHistory(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	if userID == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"user ID is required",
		))
		return
	}

	// Parse pagination parameters
	limit := h.parseIntQuery(r, "limit", 50)   // Default 50
	skip := h.parseIntQuery(r, "skip", 0)      // Default 0

	// Validate pagination parameters
	if limit > 1000 {
		limit = 1000 // Cap at 1000 records
	}
	if limit < 1 {
		limit = 50
	}
	if skip < 0 {
		skip = 0
	}

	usage, err := h.usageService.GetUserUsageHistory(r.Context(), userID, limit, skip)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to get user usage history: "+err.Error(),
		))
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
		"usage_history": usage,
		"total_records": len(usage),
		"pagination": map[string]interface{}{
			"limit": limit,
			"skip": skip,
		},
	})
}

// NEW: GetServiceUsageHistory method
func (h *UsageHandler) GetServiceUsageHistory(w http.ResponseWriter, r *http.Request) {
	serviceName := chi.URLParam(r, "serviceName")
	if serviceName == "" {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"service name is required",
		))
		return
	}

	// Parse pagination parameters
	limit := h.parseIntQuery(r, "limit", 50)   // Default 50
	skip := h.parseIntQuery(r, "skip", 0)      // Default 0

	// Validate pagination parameters
	if limit > 1000 {
		limit = 1000 // Cap at 1000 records
	}
	if limit < 1 {
		limit = 50
	}
	if skip < 0 {
		skip = 0
	}

	usage, err := h.usageService.GetServiceUsageHistory(r.Context(), serviceName, limit, skip)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to get service usage history: "+err.Error(),
		))
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"service_name": serviceName,
		"usage_history": usage,
		"total_records": len(usage),
		"pagination": map[string]interface{}{
			"limit": limit,
			"skip": skip,
		},
	})
}

// Helper method to parse date range from query parameters
func (h *UsageHandler) parseDateRange(r *http.Request) (*time.Time, *time.Time) {
	var startDate, endDate *time.Time
	
	if startStr := r.URL.Query().Get("start_date"); startStr != "" {
		if parsed, err := time.Parse("2006-01-02", startStr); err == nil {
			startDate = &parsed
		}
	}
	
	if endStr := r.URL.Query().Get("end_date"); endStr != "" {
		if parsed, err := time.Parse("2006-01-02", endStr); err == nil {
			// Add 23:59:59 to include the entire end date
			endTime := parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			endDate = &endTime
		}
	}
	
	return startDate, endDate
}

// Helper method to parse integer query parameters with default values
func (h *UsageHandler) parseIntQuery(r *http.Request, key string, defaultValue int) int {
	if str := r.URL.Query().Get(key); str != "" {
		if val, err := strconv.Atoi(str); err == nil {
			return val
		}
	}
	return defaultValue
}