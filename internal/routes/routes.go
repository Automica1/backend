// Updated internal/routes/routes.go
package routes

import (
	"time"

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
}

// Services struct to hold required services for middleware
type Services struct {
	APIKeyService services.APIKeyService
}

func SetupRoutes(h *Handlers, s *Services) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Logger())
	r.Use(middleware.Recoverer())
	r.Use(middleware.RequestID())
	r.Use(middleware.RealIP())
	r.Use(middleware.Timeout(90 * time.Second))
	r.Use(middleware.CORS())

	// Health check routes
	r.Get("/", h.Health.HealthCheck)
	r.Get("/health", h.Health.HealthCheck)

	// Debug route (NO AUTH - for easy testing)
	r.Get("/debug/token", h.Debug.ShowTokenData)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no authentication required)
		r.Group(func(r chi.Router) {
			r.Post("/register", h.User.RegisterUser)
		})

		// Protected routes (JWT authentication required)
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

			// API Key management routes (JWT auth required)
			// Updated for single API key per user system
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

			// API key validation endpoint (for debugging/external use)
			r.Route("/validate", func(r chi.Router) {
				// Validate API key format and status
				r.Post("/api-key", h.APIKey.ValidateAPIKey)
			})
		})

		// Routes that support both JWT and API Key authentication
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

		// Optional: API-only routes (only accessible with API keys, not JWT)
		// Uncomment if you want some endpoints to be API-key only
		/*
		r.Group(func(r chi.Router) {
			r.Use(middleware.APIKeyAuth(s.APIKeyService))
			
			// Example API-only endpoints
			// r.Post("/webhook", h.SomeHandler.HandleWebhook)
			// r.Post("/integration", h.SomeHandler.HandleIntegration)
		})
		*/
	})

	return r
}