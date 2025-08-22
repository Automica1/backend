// Updated internal/routes/routes.go
package routes

import (
	"time"

	"chi-mongo-backend/internal/handlers"
	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/services"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
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
}

// Services struct to hold required services for middleware
type Services struct {
	APIKeyService services.APIKeyService
}

func SetupRoutes(h *Handlers, s *Services) *chi.Mux {
	logger := zap.L().With(zap.String("component", "routes"))
	logger.Info("🔧 Setting up routes and middleware")

	r := chi.NewRouter()

	// Global middleware
	logger.Debug("Adding global middleware")
	r.Use(middleware.Logger())
	r.Use(middleware.Recoverer())
	r.Use(middleware.RequestID())
	r.Use(middleware.RealIP())
	r.Use(middleware.Timeout(90 * time.Second))
	r.Use(middleware.CORS())
	logger.Info("✅ Global middleware configured", 
		zap.Duration("timeout", 90*time.Second),
	)

	// Health check routes
	logger.Debug("Setting up health check routes")
	r.Get("/", h.Health.HealthCheck)
	r.Get("/health", h.Health.HealthCheck)
	logger.Info("✅ Health check routes configured", 
		zap.Strings("endpoints", []string{"GET /", "GET /health"}),
	)

	// Debug route (NO AUTH - for easy testing)
	logger.Debug("Setting up debug routes")
	r.Get("/debug/token", h.Debug.ShowTokenData)
	logger.Info("✅ Debug routes configured", 
		zap.Strings("endpoints", []string{"GET /debug/token"}),
		zap.Bool("auth_required", false),
	)

	// API routes
	logger.Debug("Setting up API v1 routes")
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no authentication required)
		logger.Debug("Setting up public routes")
		r.Group(func(r chi.Router) {
			r.Post("/register", h.User.RegisterUser)
		})
		logger.Info("✅ Public routes configured", 
			zap.Strings("endpoints", []string{"POST /api/v1/register"}),
			zap.Bool("auth_required", false),
		)

		// Protected routes (JWT authentication required)
		logger.Debug("Setting up JWT-protected routes")
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth())
			
			// Credits routes with different authorization levels
			r.Route("/credits", func(r chi.Router) {
				// GET balance - accessible to all authenticated users
				r.Get("/balance", h.Credits.GetBalance)
				
				// POST deduct credits - accessible to all authenticated users
				r.Post("/deduct", h.Credits.DeductCredits)
				
				// POST add credits - only accessible to admins
				r.With(middleware.AdminOnly()).Post("/add", h.Credits.AddCredits)
			})
			logger.Info("✅ Credits routes configured", 
				zap.Strings("endpoints", []string{
					"GET /api/v1/credits/balance",
					"POST /api/v1/credits/deduct", 
					"POST /api/v1/credits/add (admin only)",
				}),
			)

			r.Route("/tokens", func(r chi.Router) {
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
			})
			logger.Info("✅ Token routes configured", 
				zap.Strings("user_endpoints", []string{
					"POST /api/v1/tokens/redeem",
				}),
				zap.Strings("admin_endpoints", []string{
					"POST /api/v1/tokens/generate",
					"GET /api/v1/tokens/my-tokens",
					"GET /api/v1/tokens/all",
					"GET /api/v1/tokens/used", 
					"GET /api/v1/tokens/unused",
					"DELETE /api/v1/tokens/{tokenId}",
				}),
			)

			// API Key management routes (JWT auth required)
			logger.Debug("Setting up API key management routes")
			r.Route("/api-keys", func(r chi.Router) {
				// Create new API key (replaces any existing key)
				r.Post("/", h.APIKey.CreateAPIKey)
				
				// Get user's API key (single key)
				r.Get("/", h.APIKey.GetAPIKey)
				
				// Backward compatibility: List API keys (returns single key in array format)
				// Deprecated: Use GET /api-keys instead
				r.Get("/list", h.APIKey.GetAPIKeys)
				
				// Update user's API key (no keyId needed since user has only one key)
				r.Put("/", h.APIKey.UpdateAPIKey)
				
				// Revoke user's API key (no keyId needed since user has only one key)
				r.Delete("/", h.APIKey.RevokeAPIKey)
				
				// Get API key statistics
				r.Get("/stats", h.APIKey.GetAPIKeyStats)
			})
			logger.Info("✅ API key management routes configured", 
				zap.Strings("endpoints", []string{
					"POST /api/v1/api-keys",
					"GET /api/v1/api-keys",
					"GET /api/v1/api-keys/list (deprecated)",
					"PUT /api/v1/api-keys",
					"DELETE /api/v1/api-keys",
					"GET /api/v1/api-keys/stats",
				}),
				zap.String("note", "Single API key per user system"),
			)

			// API key validation endpoint (for debugging/external use)
			r.Route("/validate", func(r chi.Router) {
				// Validate API key format and status
				r.Post("/api-key", h.APIKey.ValidateAPIKey)
			})
			logger.Debug("✅ API key validation routes configured")

			// Admin-only user management routes
			logger.Debug("Setting up admin-only user management routes")
			r.Route("/admin", func(r chi.Router) {
				r.Use(middleware.AdminOnly())
				
				// User management endpoints
				r.Route("/users", func(r chi.Router) {
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
				})
			})
			logger.Info("✅ Admin user management routes configured", 
				zap.Strings("endpoints", []string{
					"GET /api/v1/admin/users",
					"GET /api/v1/admin/users/{userId}",
					"GET /api/v1/admin/users/stats",
					"GET /api/v1/admin/users/{userId}/activity",
					"GET /api/v1/admin/users/{userId}/credits",
				}),
				zap.String("auth_level", "admin_only"),
			)
		})

		// Routes that support both JWT and API Key authentication
		logger.Debug("Setting up dual authentication routes (JWT + API Key)")
		if s.APIKeyService == nil {
			logger.Error("⚠️ APIKeyService is nil, dual auth routes may not work properly")
		}
		
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthOrAPIKey(s.APIKeyService)) // Pass the API key service
			
			// API processing routes - accessible with either JWT or API key
			r.Post("/qr-masking", h.QRMasking.ProcessQRMasking)
			r.Post("/qr-extraction", h.QRExtraction.ProcessQRExtraction)
			r.Post("/id-cropping", h.IDCropping.ProcessIDCropping)
			r.Post("/signature-verification", h.SignatureVerification.ProcessSignatureVerification)
			r.Post("/face-detect", h.FaceDetect.ProcessFaceDetection)
			r.Post("/face-verification", h.FaceVerify.ProcessFaceVerification)
		})
		
		processingEndpoints := []string{
			"POST /api/v1/qr-masking",
			"POST /api/v1/qr-extraction", 
			"POST /api/v1/id-cropping",
			"POST /api/v1/signature-verification",
			"POST /api/v1/face-detect",
			"POST /api/v1/face-verification",
		}
		logger.Info("✅ API processing routes configured", 
			zap.Strings("endpoints", processingEndpoints),
			zap.String("auth_type", "jwt_or_api_key"),
			zap.String("note", "Supports both Bearer token and API key authentication"),
		)

		// Optional: API-only routes (only accessible with API keys, not JWT)
		// Uncomment if you want some endpoints to be API-key only
		/*
		logger.Debug("Setting up API-key-only routes")
		r.Group(func(r chi.Router) {
			r.Use(middleware.APIKeyAuth(s.APIKeyService))
			
			// Example API-only endpoints
			// r.Post("/webhook", h.SomeHandler.HandleWebhook)
			// r.Post("/integration", h.SomeHandler.HandleIntegration)
		})
		logger.Info("✅ API-key-only routes configured")
		*/
	})

	// Log route setup completion with summary
	logger.Info("🎉 Route setup completed successfully",
		zap.Int("total_route_groups", 7), // Approximate count
		zap.Bool("cors_enabled", true),
		zap.Bool("request_logging_enabled", true),
		zap.Bool("panic_recovery_enabled", true),
		zap.Duration("request_timeout", 90*time.Second),
		zap.String("api_version", "v1"),
	)

	return r
}