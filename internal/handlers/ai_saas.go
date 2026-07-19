package handlers

import (
	"net/http"
	"time"

	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type AISaaSHandler struct {
	aiSaaSService services.AISaaSService
}

func NewAISaaSHandler(aiSaaSService services.AISaaSService) *AISaaSHandler {
	return &AISaaSHandler{aiSaaSService: aiSaaSService}
}

func (h *AISaaSHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	startDate, endDate, err := parseAISaaSDateRange(r)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			err.Error(),
		))
		return
	}

	response, err := h.aiSaaSService.ListServices(r.Context(), startDate, endDate)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrInternalServer,
			http.StatusInternalServerError,
			"failed to build AI SaaS service ledger: "+err.Error(),
		))
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

func parseAISaaSDateRange(r *http.Request) (*time.Time, *time.Time, error) {
	var startDate, endDate *time.Time
	if value := r.URL.Query().Get("start_date"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, nil, errInvalidAISaaSDate("start_date")
		}
		startDate = &parsed
	}
	if value := r.URL.Query().Get("end_date"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, nil, errInvalidAISaaSDate("end_date")
		}
		end := parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		endDate = &end
	}
	if startDate != nil && endDate != nil && endDate.Before(*startDate) {
		return nil, nil, errInvalidAISaaSRange()
	}
	return startDate, endDate, nil
}

func errInvalidAISaaSDate(field string) error {
	return &aiSaaSValidationError{message: field + " must use YYYY-MM-DD"}
}

func errInvalidAISaaSRange() error {
	return &aiSaaSValidationError{message: "end_date must be on or after start_date"}
}

type aiSaaSValidationError struct {
	message string
}

func (e *aiSaaSValidationError) Error() string {
	return e.message
}
