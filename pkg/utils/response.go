// pkg/utils/response.go
package utils

import (
	"encoding/json"
	"net/http"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

// SendJSONResponse sends a JSON response
func SendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func SendErrorResponse(w http.ResponseWriter, err error) {
	statusCode := apperrors.GetHTTPStatusCode(err)
	response := models.ErrorResponse{
		Error: err.Error(),
	}
	SendJSONResponse(w, statusCode, response)
}

func DecodeJSONBody(r *http.Request, dst interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return apperrors.NewAppError(apperrors.ErrBadRequest, http.StatusBadRequest, "invalid JSON format")
	}
	return nil
}