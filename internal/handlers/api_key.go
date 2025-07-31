// internal/handlers/api_key.go
package handlers

import (
	"net/http"
	"time"

	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type APIKeyHandler struct {
	apiKeyService services.APIKeyService
	userService   services.UserService
}

func NewAPIKeyHandler(apiKeyService services.APIKeyService, userService services.UserService) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
		userService:   userService,
	}
}

func (h *APIKeyHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	// Get user info from context (set by auth middleware)
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get or create user
	user, err := h.userService.GetOrCreateUser(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Create default request
	var req models.CreateAPIKeyRequest
	
	// Try to decode JSON body, but don't fail if it's empty or missing
	if r.ContentLength > 0 {
		if err := utils.DecodeJSONBody(r, &req); err != nil {
			utils.SendErrorResponse(w, err)
			return
		}
	}
	
	// Set defaults if not provided
	if req.KeyName == "" {
		req.KeyName = "My API Key"
	}
	
	if req.ExpiresAt == nil {
		// Set default expiry to 30 days from now
		expiresAt := time.Now().AddDate(0, 0, 30)
		req.ExpiresAt = &expiresAt
	}

	// This will now delete any existing key and create a new one
	response, err := h.apiKeyService.CreateAPIKey(r.Context(), user.UserID, email, &req)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusCreated, response)
}

// GetAPIKey returns the user's single API key (renamed from GetAPIKeys)
func (h *APIKeyHandler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	// Get user info from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get user
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.apiKeyService.GetUserAPIKey(r.Context(), user.UserID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

// Keep GetAPIKeys for backward compatibility (delegates to GetAPIKey)
// Deprecated: Use GetAPIKey instead
func (h *APIKeyHandler) GetAPIKeys(w http.ResponseWriter, r *http.Request) {
	// Get user info from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get user
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	keyResponse, err := h.apiKeyService.GetUserAPIKey(r.Context(), user.UserID)
	if err != nil {
		// If no key found, return empty list for backward compatibility
		// Check if it's a "not found" error by examining the error message
		if err.Error() == "api key not found" || err.Error() == "no documents in result" {
			response := &models.APIKeyListResponse{
				Message: "API keys retrieved successfully",
				Keys:    []models.APIKey{},
				Total:   0,
			}
			utils.SendJSONResponse(w, http.StatusOK, response)
			return
		}
		utils.SendErrorResponse(w, err)
		return
	}

	// Convert single key response to list format for backward compatibility
	var keys []models.APIKey
	if keyResponse.Key != nil {
		keys = []models.APIKey{*keyResponse.Key}
	}

	response := &models.APIKeyListResponse{
		Message: "API keys retrieved successfully",
		Keys:    keys,
		Total:   len(keys),
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

// UpdateAPIKey updates the user's API key (no longer requires keyID in URL)
func (h *APIKeyHandler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	// Get user info from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get user
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	var req models.UpdateAPIKeyRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Update the user's single API key
	if err := h.apiKeyService.UpdateAPIKey(r.Context(), user.UserID, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{
		"message": "API key updated successfully",
	})
}

// RevokeAPIKey revokes the user's API key (no longer requires keyID in URL)
func (h *APIKeyHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	// Get user info from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get user
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	// Revoke the user's API key
	if err := h.apiKeyService.RevokeAPIKey(r.Context(), user.UserID); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, map[string]string{
		"message": "API key revoked successfully",
	})
}

// GetAPIKeyStats returns statistics for the user's API key
func (h *APIKeyHandler) GetAPIKeyStats(w http.ResponseWriter, r *http.Request) {
	// Get user info from context
	email, ok := middleware.GetEmailFromContext(r.Context())
	if !ok {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"email not found in context",
		))
		return
	}

	// Get user
	user, err := h.userService.GetUserByEmail(r.Context(), email)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	response, err := h.apiKeyService.GetAPIKeyStats(r.Context(), user.UserID)
	if err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	utils.SendJSONResponse(w, http.StatusOK, response)
}

// ValidateAPIKey is a utility endpoint to check if an API key is valid
// This can be used by external services or for debugging
func (h *APIKeyHandler) ValidateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req models.APIKeyValidationRequest
	if err := utils.DecodeJSONBody(r, &req); err != nil {
		utils.SendErrorResponse(w, err)
		return
	}

	if err := req.Validate(); err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrValidation,
			http.StatusBadRequest,
			"validation failed",
			err.Error(),
		))
		return
	}

	apiKeyRecord, err := h.apiKeyService.ValidateAPIKey(r.Context(), req.APIKey)
	if err != nil {
		utils.SendErrorResponse(w, apperrors.NewAppError(
			apperrors.ErrUnauthorized,
			http.StatusUnauthorized,
			"invalid or expired API key",
		))
		return
	}

	// Return sanitized API key info
	sanitized := apiKeyRecord.Sanitize()
	utils.SendJSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "API key is valid",
		"valid":   true,
		"key":     sanitized,
	})
}