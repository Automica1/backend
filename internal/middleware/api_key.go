// internal/middleware/api_key.go
package middleware

import (
	"context"
	"net/http"
	"strings"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

type contextKey string

const (
	APIKeyContextKey contextKey = "api_key"
	UserIDContextKey contextKey = "user_id"
	// EmailContextKey is defined in auth.go, don't redeclare it here
)

// APIKeyAuth middleware validates API keys
func APIKeyAuth(apiKeyService services.APIKeyService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API key from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"authorization header required",
				))
				return
			}

			// Check for API key format: "Bearer ak_live_..."
			if !strings.HasPrefix(authHeader, "Bearer ") {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"invalid authorization header format",
				))
				return
			}

			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			
			// Validate API key format
			if !strings.HasPrefix(apiKey, "ak_live_") {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"invalid API key format",
				))
				return
			}

			// Validate API key
			apiKeyRecord, err := apiKeyService.ValidateAPIKey(r.Context(), apiKey)
			if err != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"invalid or expired API key",
				))
				return
			}

			// Add API key info to context
			ctx := context.WithValue(r.Context(), APIKeyContextKey, apiKeyRecord)
			ctx = context.WithValue(ctx, UserIDContextKey, apiKeyRecord.UserID)
			ctx = context.WithValue(ctx, "email", apiKeyRecord.Email) // Use string key like in auth.go

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthOrAPIKey middleware supports both JWT and API key authentication
func AuthOrAPIKey(apiKeyService services.APIKeyService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"authorization header required",
				))
				return
			}

			if !strings.HasPrefix(authHeader, "Bearer ") {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"invalid authorization header format",
				))
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Check if it's an API key (starts with ak_live_)
			if strings.HasPrefix(token, "ak_live_") {
				// Handle API key authentication
				apiKeyRecord, err := apiKeyService.ValidateAPIKey(r.Context(), token)
				if err != nil {
					utils.SendErrorResponse(w, apperrors.NewAppError(
						apperrors.ErrUnauthorized,
						http.StatusUnauthorized,
						"invalid or expired API key",
					))
					return
				}

				// Add API key info to context
				ctx := context.WithValue(r.Context(), APIKeyContextKey, apiKeyRecord)
				ctx = context.WithValue(ctx, UserIDContextKey, apiKeyRecord.UserID)
				ctx = context.WithValue(ctx, "email", apiKeyRecord.Email) // Use string key like in auth.go

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Otherwise, handle JWT authentication using the verifyKindeToken function from auth.go
			claims, err := verifyKindeToken(token)
			if err != nil {
				utils.SendErrorResponse(w, apperrors.NewAppError(
					apperrors.ErrUnauthorized,
					http.StatusUnauthorized,
					"invalid or expired token",
				))
				return
			}

			// Add user info to context (same as auth.go)
			ctx := context.WithValue(r.Context(), "email", claims.Email)
			ctx = context.WithValue(ctx, "isAdmin", isUserAdmin(claims.Roles))
			ctx = context.WithValue(ctx, "roles", claims.Roles)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Helper functions to extract values from context
func GetAPIKeyFromContext(ctx context.Context) (*models.APIKey, bool) {
	apiKey, ok := ctx.Value(APIKeyContextKey).(*models.APIKey)
	return apiKey, ok
}

func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDContextKey).(string)
	return userID, ok
}

// Remove the duplicate GetEmailFromContext function - it's already defined in auth.go