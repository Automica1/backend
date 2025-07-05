// pkg/utils/response.go
package utils

import (
	"encoding/json"
	"net/http"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

// Enhanced ErrorResponse with user-friendly fields
type EnhancedErrorResponse struct {
	Error            string `json:"error"`
	UserMessage      string `json:"user_message,omitempty"`
	TechnicalMessage string `json:"technical_message,omitempty"`
	Suggestion       string `json:"suggestion,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
}

// SendJSONResponse sends a JSON response
func SendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// SendErrorResponse sends an enhanced error response with user-friendly messaging
func SendErrorResponse(w http.ResponseWriter, err error) {
	statusCode := apperrors.GetHTTPStatusCode(err)
	
	// Check if it's our enhanced AppError
	if appErr, ok := err.(*apperrors.AppError); ok && appErr.UserMessage != "" {
		// Send enhanced error response with user-friendly fields
		response := EnhancedErrorResponse{
			Error:            appErr.Message,
			UserMessage:      appErr.UserMessage,
			TechnicalMessage: appErr.TechnicalMessage,
			Suggestion:       appErr.Suggestion,
			ErrorCode:        appErr.ErrorCode,
		}
		SendJSONResponse(w, statusCode, response)
		return
	}
	
	// Fallback to simple error response for backward compatibility
	response := models.ErrorResponse{
		Error: err.Error(),
	}
	SendJSONResponse(w, statusCode, response)
}

// SendUserFriendlyErrorResponse sends a user-friendly error response
func SendUserFriendlyErrorResponse(w http.ResponseWriter, userMessage, technicalMessage, suggestion, errorCode string, statusCode int) {
	response := EnhancedErrorResponse{
		Error:            userMessage,
		UserMessage:      userMessage,
		TechnicalMessage: technicalMessage,
		Suggestion:       suggestion,
		ErrorCode:        errorCode,
	}
	SendJSONResponse(w, statusCode, response)
}

func DecodeJSONBody(r *http.Request, dst interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return apperrors.NewAppError(apperrors.ErrBadRequest, http.StatusBadRequest, "invalid JSON format")
	}
	return nil
}