// internal/routes/routes.go
package routes

import (
	"time"

	"go.uber.org/zap"

	"chi-mongo-backend/internal/handlers"
	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/services"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	Health                *handlers.HealthHandler
	User                  *handlers.UserHandler
	Credits               *handlers.CreditsHandler
	QRMasking             *handlers.QRMaskingHandler
	QRExtraction          *handlers.QRExtractionHandler
	IDCropping            *handlers.IDCroppingHandler
	SignatureVerification *handlers.SignatureVerificationHandler
	FaceDetect            *handlers.FaceDetectionHandler
	FaceVerify            *handlers.FaceVerificationHandler
	Debug                 *handlers.DebugHandler
	Token                 *handlers.TokenHandler
	APIKey                *handlers.APIKeyHandler
	Usage                 *handlers.UsageHandler
}

// Services struct to hold required services for middleware
type Services struct {
	APIKeyService services.APIKeyService
	UsageService  services.UsageService
}

func SetupRoutes(h *Handlers, s *Services) *chi.Mux {
	logger := zap.L() // Get the global logger
	
	logger.Info("Setting up routes")
	
	r := chi.NewRouter()

	// Global middleware
	logger.Debug("Configuring global middleware")
	r.Use(middleware.Logger())
	r.Use(middleware.Recoverer())
	r.Use(middleware.RequestID())
	r.Use(middleware.RealIP())
	r.Use(middleware.Timeout(90 * time.Second))
	r.Use(middleware.CORS())

	logger.Info("Global middleware configured",
		zap.Duration("timeout", 90*time.Second),
		zap.Bool("cors_enabled", true))

	// Health check routes
	logger.Debug("Setting up health check routes")
	r.Get("/", h.Health.HealthCheck)
	r.Get("/health", h.Health.HealthCheck)

	// Debug route (NO AUTH - for easy testing)
	logger.Debug("Setting up debug routes")
	r.Get("/debug/token", h.Debug.ShowTokenData)

	logger.Warn("Debug endpoint enabled without authentication",
		zap.String("endpoint", "/debug/token"),
		zap.String("warning", "This endpoint should be disabled in production"))

	// API routes
	logger.Debug("Setting up API v1 routes")
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no authentication required)
		r.Group(func(r chi.Router) {
			logger.Debug("Setting up public routes")
			r.Post("/register", h.User.RegisterUser)
			
			logger.Info("Public routes configured",
				zap.Strings("endpoints", []string{"/api/v1/register"}))
		})

		// Protected routes (JWT authentication required)
		r.Group(func(r chi.Router) {
			logger.Debug("Setting up JWT protected routes")
			r.Use(middleware.Auth())
			
			// Credits routes with different authorization levels
			r.Route("/credits", func(r chi.Router) {
				logger.Debug("Setting up credits routes")
				
				// GET balance - accessible to all authenticated users
				r.Get("/balance", h.Credits.GetBalance)
				
				// POST deduct credits - accessible to all authenticated users
				r.Post("/deduct", h.Credits.DeductCredits)
				
				// POST add credits - only accessible to admins
				r.With(middleware.AdminOnly()).Post("/add", h.Credits.AddCredits)
				
				logger.Info("Credits routes configured",
					zap.Strings("user_endpoints", []string{"/balance", "/deduct"}),
					zap.Strings("admin_endpoints", []string{"/add"}))
			})

			r.Route("/tokens", func(r chi.Router) {
				logger.Debug("Setting up token routes")
				
				// POST generate token - only accessible to admins
				r.With(middleware.AdminOnly()).Post("/generate", h.Token.GenerateToken)
				
				// POST redeem token - accessible to all authenticated users
				r.Post("/redeem", h.Token.RedeemToken)
				
				// Admin-only token viewing routes
				r.Group(func(r chi.Router) {
					r.Use(middleware.AdminOnly())
					
					// GET my tokens - see tokens created by the current admin
					r.Get("/my-tokens", h.Token.GetMyTokens)
					
					// GET all tokens - see all tokens in the system
					r.Get("/all", h.Token.GetAllTokens)
					
					// GET used tokens - see all tokens that have been redeemed
					r.Get("/used", h.Token.GetUsedTokens)
					
					// GET unused tokens - see all tokens that haven't been redeemed yet
					r.Get("/unused", h.Token.GetUnusedTokens)

					r.Delete("/{tokenId}", h.Token.DeleteToken)
				})
				
				logger.Info("Token routes configured",
					zap.Strings("user_endpoints", []string{"/redeem"}),
					zap.Strings("admin_endpoints", []string{"/generate", "/my-tokens", "/all", "/used", "/unused", "/{tokenId}"}))
			})

			// API Key management routes (JWT auth required)
			r.Route("/api-keys", func(r chi.Router) {
				logger.Debug("Setting up API key management routes")
				
				// Create new API key (replaces any existing key)
				r.Post("/", h.APIKey.CreateAPIKey)
				
				// Get user's API key (single key)
				r.Get("/", h.APIKey.GetAPIKey)
				
				// Backward compatibility: List API keys (returns single key in array format)
				r.Get("/list", h.APIKey.GetAPIKeys)
				
				// Update user's API key (no keyId needed since user has only one key)
				r.Put("/", h.APIKey.UpdateAPIKey)
				
				// Revoke user's API key (no keyId needed since user has only one key)
				r.Delete("/", h.APIKey.RevokeAPIKey)
				
				// Get API key statistics
				r.Get("/stats", h.APIKey.GetAPIKeyStats)
				
				logger.Info("API key management routes configured",
					zap.Strings("endpoints", []string{"/", "/list", "/stats"}),
					zap.String("note", "Single API key per user model"))
			})

			// API key validation endpoint (for debugging/external use)
			r.Route("/validate", func(r chi.Router) {
				logger.Debug("Setting up validation routes")
				
				// Validate API key format and status
				r.Post("/api-key", h.APIKey.ValidateAPIKey)
				
				logger.Info("Validation routes configured",
					zap.Strings("endpoints", []string{"/api-key"}))
			})

			// Admin-only user management routes
			r.Route("/admin", func(r chi.Router) {
				logger.Debug("Setting up admin routes")
				r.Use(middleware.AdminOnly())
				
				// User management endpoints
				r.Route("/users", func(r chi.Router) {
					logger.Debug("Setting up admin user management routes")
					
					// GET all users - list all users in the system
					r.Get("/", h.User.GetAllUsers)
					
					// GET specific user - get user details by ID
					r.Get("/{userId}", h.User.GetUserByID)
					
					// GET user stats - get aggregated user statistics
					r.Get("/stats", h.User.GetUserStats)
					
					// GET user's activity log - get activity history for a specific user
					r.Get("/{userId}/activity", h.User.GetUserActivity)
					
					// GET user's credits - get credit balance for a specific user
					r.Get("/{userId}/credits", h.User.GetUserCredits)
					
					logger.Info("Admin user management routes configured",
						zap.Strings("endpoints", []string{"/", "/{userId}", "/stats", "/{userId}/activity", "/{userId}/credits"}),
						zap.String("access", "admin_only"))
				})

				// Usage tracking endpoints (Admin only)
				r.Route("/usage", func(r chi.Router) {
					logger.Debug("Setting up admin usage tracking routes")
					
					// Global service usage statistics
					// GET /api/v1/admin/usage/global?start_date=2024-01-01&end_date=2024-01-31
					r.Get("/global", h.Usage.GetGlobalStats)
					
					// Per-user usage statistics
					// GET /api/v1/admin/usage/users?start_date=2024-01-01&end_date=2024-01-31
					r.Get("/users", h.Usage.GetUserStats)
					
					// Service-specific user statistics
					// GET /api/v1/admin/usage/services?service=signature-verification&start_date=2024-01-01
					r.Get("/services", h.Usage.GetServiceUserStats)
					
					// Individual user's usage history
					// GET /api/v1/admin/usage/user/{userId}/history?limit=50&skip=0
					r.Get("/user/{userId}/history", h.Usage.GetUserUsageHistory)
					
					// Service-specific usage history
					// GET /api/v1/admin/usage/service/{serviceName}/history?limit=50&skip=0
					r.Get("/service/{serviceName}/history", h.Usage.GetServiceUsageHistory)
					
					logger.Info("Admin usage tracking routes configured",
						zap.Strings("endpoints", []string{"/global", "/users", "/services", "/user/{userId}/history", "/service/{serviceName}/history"}),
						zap.String("access", "admin_only"),
						zap.String("feature", "usage_analytics"))
				})
				
				logger.Info("All admin routes configured successfully")
			})
			
			logger.Info("All JWT protected routes configured successfully")
		})

		// Routes that support both JWT and API Key authentication
		r.Group(func(r chi.Router) {
			logger.Debug("Setting up dual authentication routes (JWT or API Key)")
			r.Use(middleware.AuthOrAPIKey(s.APIKeyService)) // Pass the API key service
			
			// API processing routes - accessible with either JWT or API key
			// These routes will automatically track usage via the handlers
			r.Post("/qr-masking", h.QRMasking.ProcessQRMasking)
			r.Post("/qr-extraction", h.QRExtraction.ProcessQRExtraction)
			r.Post("/id-cropping", h.IDCropping.ProcessIDCropping)
			r.Post("/signature-verification", h.SignatureVerification.ProcessSignatureVerification)
			r.Post("/face-detect", h.FaceDetect.ProcessFaceDetection)
			r.Post("/face-verification", h.FaceVerify.ProcessFaceVerification)
			
			// Log API processing endpoints with their usage tracking status
			apiEndpoints := []struct {
				path     string
				tracking bool
			}{
				{"/qr-masking", false},
				{"/qr-extraction", false},
				{"/id-cropping", false},
				{"/signature-verification", true},
				{"/face-detect", false},
				{"/face-verification", false},
			}
			
			for _, endpoint := range apiEndpoints {
				logger.Info("API processing endpoint configured",
					zap.String("endpoint", endpoint.path),
					zap.Bool("usage_tracking", endpoint.tracking),
					zap.Strings("auth_methods", []string{"JWT", "API_KEY"}))
			}
			
			logger.Info("All dual authentication routes configured successfully",
				zap.Int("endpoint_count", len(apiEndpoints)),
				zap.String("auth_types", "JWT or API Key"))
		})

		// Optional: API-only routes (only accessible with API keys, not JWT)
		// Uncomment if you want some endpoints to be API-key only
		/*
		logger.Debug("API-only routes are commented out")
		r.Group(func(r chi.Router) {
			r.Use(middleware.APIKeyAuth(s.APIKeyService))
			
			// Example API-only endpoints
			// r.Post("/webhook", h.SomeHandler.HandleWebhook)
			// r.Post("/integration", h.SomeHandler.HandleIntegration)
			
			logger.Info("API-only routes would be configured here")
		})
		*/
		
		logger.Info("All API v1 routes configured successfully")
	})

	// Log route summary
	logger.Info("Route setup completed",
		zap.String("api_version", "v1"),
		zap.Bool("health_checks", true),
		zap.Bool("debug_endpoints", true),
		zap.Bool("public_registration", true),
		zap.Bool("jwt_auth", true),
		zap.Bool("api_key_auth", true),
		zap.Bool("admin_endpoints", true),
		zap.Bool("usage_tracking", true),
		zap.Bool("cors_enabled", true))

	return r
}