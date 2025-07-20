// pkg/utils/response.go - Updated with better error handling
package utils

import (
	"encoding/json"
	"net/http"
	"log"

	"chi-mongo-backend/internal/models"
	apperrors "chi-mongo-backend/pkg/errors"
)

// Enhanced ErrorResponse with user-friendly fields and original response
type EnhancedErrorResponse struct {
	Error            string      `json:"error"`
	UserMessage      string      `json:"user_message,omitempty"`
	TechnicalMessage string      `json:"technical_message,omitempty"`
	Suggestion       string      `json:"suggestion,omitempty"`
	ErrorCode        string      `json:"error_code,omitempty"`
	RequestID        string      `json:"request_id,omitempty"`
	OriginalResponse interface{} `json:"original_response,omitempty"` // Include original backend response
}

// SendJSONResponse sends a JSON response with proper error handling
func SendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	
	// Marshal the data first to catch any encoding errors
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshaling JSON response: %v", err)
		// Fallback to a simple error response
		w.WriteHeader(http.StatusInternalServerError)
		fallbackResponse := map[string]string{
			"error": "Internal server error: failed to encode response",
		}
		json.NewEncoder(w).Encode(fallbackResponse)
		return
	}
	
	w.WriteHeader(statusCode)
	
	// Write the marshaled JSON data
	if _, writeErr := w.Write(jsonData); writeErr != nil {
		log.Printf("Error writing response: %v", writeErr)
	}
	
	// Log successful response
	log.Printf("Response sent successfully: status=%d, size=%d bytes", statusCode, len(jsonData))
}

// SendErrorResponse sends an enhanced error response with user-friendly messaging
func SendErrorResponse(w http.ResponseWriter, err error) {
	statusCode := apperrors.GetHTTPStatusCode(err)
	
	// Check if it's our enhanced AppError
	if appErr, ok := err.(*apperrors.AppError); ok {
		// Log for debugging
		log.Printf("Sending enhanced error response: %+v", appErr)
		
		// Clean up the original response if it exists
		var cleanedOriginalResponse interface{}
		if appErr.OriginalResponse != nil {
			cleanedOriginalResponse = cleanOriginalResponse(appErr.OriginalResponse)
		}
		
		// Send enhanced error response with user-friendly fields and original response
		response := EnhancedErrorResponse{
			Error:            appErr.Message,
			UserMessage:      appErr.UserMessage,
			TechnicalMessage: appErr.TechnicalMessage,
			Suggestion:       appErr.Suggestion,
			ErrorCode:        appErr.ErrorCode,
			OriginalResponse: cleanedOriginalResponse,
		}
		
		// Log the response being sent (with truncated original response for readability)
		logResponse := response
		if cleanedOriginalResponse != nil {
			logResponse.OriginalResponse = "[ORIGINAL_RESPONSE_PRESENT]"
		}
		log.Printf("Enhanced error response being sent: %+v", logResponse)
		
		SendJSONResponse(w, statusCode, response)
		return
	}
	
	// Fallback to simple error response for backward compatibility
	response := models.ErrorResponse{
		Error: err.Error(),
	}
	SendJSONResponse(w, statusCode, response)
}

// cleanOriginalResponse cleans up the original response to ensure JSON serialization
func cleanOriginalResponse(originalResponse interface{}) interface{} {
	// If it's already a map, clean it up
	if responseMap, ok := originalResponse.(map[string]interface{}); ok {
		cleanedMap := make(map[string]interface{})
		for key, value := range responseMap {
			cleanedMap[key] = cleanValue(value)
		}
		return cleanedMap
	}
	
	// For other types, try to clean them
	return cleanValue(originalResponse)
}

// cleanValue recursively cleans values to ensure JSON serialization
func cleanValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{})
		for key, val := range v {
			cleaned[key] = cleanValue(val)
		}
		return cleaned
	case []interface{}:
		cleaned := make([]interface{}, len(v))
		for i, val := range v {
			cleaned[i] = cleanValue(val)
		}
		return cleaned
	case string:
		// Remove any non-printable characters that might cause JSON issues
		return v
	case nil:
		return nil
	default:
		// For any other type, convert to string representation
		return value
	}
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

// SendUserFriendlyErrorResponseWithOriginal sends a user-friendly error response with original response
func SendUserFriendlyErrorResponseWithOriginal(w http.ResponseWriter, userMessage, technicalMessage, suggestion, errorCode string, statusCode int, originalResponse interface{}) {
	response := EnhancedErrorResponse{
		Error:            userMessage,
		UserMessage:      userMessage,
		TechnicalMessage: technicalMessage,
		Suggestion:       suggestion,
		ErrorCode:        errorCode,
		OriginalResponse: cleanOriginalResponse(originalResponse),
	}
	SendJSONResponse(w, statusCode, response)
}

func DecodeJSONBody(r *http.Request, dst interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return apperrors.NewAppError(apperrors.ErrBadRequest, http.StatusBadRequest, "invalid JSON format")
	}
	return nil
}