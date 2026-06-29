// 8. Updated internal/routes/routes.go with usage endpoints
package routes

import (
	"time"

	"chi-mongo-backend/internal/handlers"
	"chi-mongo-backend/internal/middleware"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/internal/services"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	Health                *handlers.HealthHandler
	Admin                 *handlers.AdminHandler
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
	BetaKey               *handlers.BetaKeyHandler
	BetaFeedback          *handlers.BetaFeedbackHandler
	GuestPass             *handlers.GuestPassHandler
	Usage                 *handlers.UsageHandler        // Add usage handler
	Subscription          *handlers.SubscriptionHandler // Add subscription handler
	Plan                  *handlers.PlanHandler         // Add plan handler
}

// Services struct to hold required services for middleware
type Services struct {
	APIKeyService    services.APIKeyService
	GuestPassService services.GuestPassService
	UsageService     services.UsageService // Add usage service
	UserRepo         repository.UserRepository
}

func SetupRoutes(h *Handlers, s *Services) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID())
	r.Use(middleware.RealIP())
	r.Use(middleware.Logger())
	r.Use(middleware.Recoverer())
	r.Use(middleware.Timeout(90 * time.Second))
	r.Use(middleware.CORS())

	// Health check routes
	r.Get("/", h.Health.HealthCheck)
	r.Get("/health", h.Health.HealthCheck)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no authentication required)
		r.Group(func(r chi.Router) {
			r.Post("/register", h.User.RegisterUser)
			r.Get("/plans", h.Plan.GetActivePlans) // Public plans list
			r.Get("/billing-config", h.Plan.GetPublicBillingConfig)

			r.Route("/guest-passes", func(r chi.Router) {
				r.Get("/balance", h.GuestPass.GetBalance)
				r.Post("/validate", h.GuestPass.ValidatePass)
			})
		})

		// Protected routes (JWT authentication required)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(s.UserRepo))

			// Credits routes with different authorization levels
			r.Route("/credits", func(r chi.Router) {
				// GET balance - accessible to all authenticated users
				r.Get("/balance", h.Credits.GetBalance)

				// POST deduct credits - accessible to all authenticated users
				r.Post("/deduct", h.Credits.DeductCredits)

				// POST add credits - only accessible to admins
				r.With(middleware.AdminOnly()).Post("/add", h.Credits.AddCredits)
			})

			// Subscription routes
			r.Route("/subscription", func(r chi.Router) {
				r.Post("/create-order", h.Subscription.CreateOrder)
				r.Post("/verify-payment", h.Subscription.VerifyPayment)
				r.Get("/status", h.Subscription.GetStatus)
				r.Get("/upgrade/calculate", h.Subscription.CalculateUpgradePrice)
				r.Post("/upgrade/create", h.Subscription.CreateUpgradeOrder)
				r.Post("/downgrade", h.Subscription.DowngradeSubscription)
				r.Post("/cancel", h.Subscription.CancelSubscription)
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
					r.Get("/my-tokens/export.csv", h.Token.ExportTokensCSV)

					// GET all tokens - see all tokens in the system
					r.Get("/all", h.Token.GetAllTokens)
					r.Get("/all/export.csv", h.Token.ExportTokensCSV)

					// GET used tokens - see all tokens that have been redeemed
					r.Get("/used", h.Token.GetUsedTokens)

					// GET unused tokens - see all tokens that haven't been redeemed yet
					r.Get("/unused", h.Token.GetUnusedTokens)

					r.Delete("/{tokenId}", h.Token.DeleteToken)
				})
			})

			// API Key management routes (JWT auth required)
			r.Route("/api-keys", func(r chi.Router) {
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
			})

			// API key validation endpoint (for debugging/external use)
			r.Route("/validate", func(r chi.Router) {
				// Validate API key format and status
				r.Post("/api-key", h.APIKey.ValidateAPIKey)
			})

			// Admin-only user management routes
			r.Route("/admin", func(r chi.Router) {
				r.Use(middleware.AdminOnly())

				// User management endpoints
				r.Route("/users", func(r chi.Router) {
					// GET all users - list all users in the system
					r.Get("/", h.User.GetAllUsers)
					r.Get("/export.csv", h.User.ExportUsersCSV)

					// GET specific user - get user details by ID
					r.Get("/{userId}", h.User.GetUserByID)
					r.Delete("/{userId}", h.User.DeleteUser)
					r.Post("/{userId}/suspend", h.User.SuspendUser)
					r.Post("/{userId}/reactivate", h.User.ReactivateUser)

					// GET user stats - get aggregated user statistics
					r.Get("/stats", h.User.GetUserStats)

					// GET user's activity log - get activity history for a specific user
					r.Get("/{userId}/activity", h.User.GetUserActivity)

					// GET user's credits - get credit balance for a specific user
					r.Get("/{userId}/credits", h.User.GetUserCredits)
				})

				// Usage tracking endpoints (Admin only)
				r.Route("/usage", func(r chi.Router) {
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
					r.Get("/history", h.Usage.GetServiceUsageHistory)
					// GET /api/v1/admin/usage/user/{userId}/history?limit=50&skip=0
					r.Get("/user/{userId}/history", h.Usage.GetUserUsageHistory)

					// Service-specific usage history
					// GET /api/v1/admin/usage/service/{serviceName}/history?limit=50&skip=0
					r.Get("/service/{serviceName}/history", h.Usage.GetServiceUsageHistory)
				})

				// Subscription management endpoints (Admin only)
				r.Route("/subscriptions", func(r chi.Router) {
					r.Get("/", h.Subscription.GetAdminSubscriptions)
					r.Get("/active-count", h.Subscription.GetActiveCount)
					r.Get("/test-reset/capabilities", h.Subscription.GetSubscriptionTestResetCapabilities)
					r.Get("/{subscriptionId}", h.Subscription.GetAdminSubscription)
					r.Post("/{subscriptionId}/reconcile", h.Subscription.ReconcileAdminSubscription)
					r.Post("/{subscriptionId}/test-reset", h.Subscription.ResetAdminSubscriptionForTesting)
				})

				// Admin intelligence and logs
				r.Get("/search", h.Admin.Search)
				r.Get("/logs", h.Admin.Logs)
				r.Get("/audit-logs/recent", h.Admin.RecentAuditLogs)
				r.Get("/summary", h.Admin.Summary)
				r.Get("/export/summary.csv", h.Admin.ExportSummaryCSV)

				// Plan management endpoints (Admin only)
				r.Route("/plans", func(r chi.Router) {
					// GET all plans (including inactive)
					r.Get("/", h.Plan.GetAllPlans)
					// POST create plan
					r.Post("/", h.Plan.CreatePlan)
					// PUT update plan
					r.Put("/{planId}", h.Plan.UpdatePlan)
					// DELETE deactivate plan
					r.Delete("/{planId}", h.Plan.DeletePlan)
				})

				// Beta key management (Admin only)
				r.Route("/beta-keys", func(r chi.Router) {
					r.Get("/services", h.BetaKey.ListSupportedServices)
					r.Get("/", h.BetaKey.ListBetaKeys)
					r.Post("/", h.BetaKey.GenerateBetaKey)
					r.Delete("/{keyId}", h.BetaKey.RevokeBetaKey)
				})

				// Beta feedback sessions (Admin only)
				r.Get("/beta-feedback/sessions", h.BetaFeedback.ListSessionsAdmin)
				r.Get("/beta-feedback/sessions/{sessionId}", h.BetaFeedback.GetSessionAdmin)

				// Guest pass management (Admin only)
				r.Route("/guest-passes", func(r chi.Router) {
					r.Get("/services", h.GuestPass.ListSupportedServices)
					r.Get("/", h.GuestPass.ListPasses)
					r.Post("/", h.GuestPass.CreatePass)
					r.Put("/{passId}", h.GuestPass.UpdatePass)
					r.Delete("/{passId}", h.GuestPass.RevokePass)
				})
			})
		})

		// Routes that support JWT, API Key, or Guest Pass authentication
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthOrAPIKeyOrGuestPass(s.APIKeyService, s.GuestPassService))

			// API processing routes - accessible with either JWT or API key
			// These routes will automatically track usage via the handlers
			r.Post("/qr-masking", h.QRMasking.ProcessQRMasking)
			r.Post("/qr-extraction", h.QRExtraction.ProcessQRExtraction)
			r.Post("/id-cropping", h.IDCropping.ProcessIDCropping)
			r.Post("/signature-verification", h.SignatureVerification.ProcessSignatureVerification)
			r.Post("/face-detect", h.FaceDetect.ProcessFaceDetection)
			r.Post("/face-verification", h.FaceVerify.ProcessFaceVerification)

			r.Get("/beta-feedback/pending", h.BetaFeedback.GetPendingFeedback)
			r.Post("/beta-feedback/{sessionId}", h.BetaFeedback.SubmitFeedback)
		})

		// Public subscription webhook
		r.Post("/subscription/webhook", h.Subscription.Webhook)

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
