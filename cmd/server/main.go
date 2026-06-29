// cmd/server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/database"
	"chi-mongo-backend/internal/handlers"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/internal/routes"
	"chi-mongo-backend/internal/services"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ Failed to load configuration: %v", err)
	}

	// Initialize database
	db, err := database.NewMongoDB(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to initialize database: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			log.Printf("❌ Error closing database connection: %v", err)
		}
	}()

	log.Println("✅ Successfully connected to MongoDB")

	// Initialize repositories
	userRepo := repository.NewUserRepository(db.GetCollection("users"))
	creditsRepo := repository.NewCreditsRepository(db.GetCollection("credits"))
	tokenRepo := repository.NewTokenRepository(db.GetCollection("tokens"))
	betaKeyRepo := repository.NewBetaKeyRepository(db.GetCollection("beta_keys"))
	guestPassRepo := repository.NewGuestPassRepository(db.GetCollection("guest_passes"))
	betaFeedbackRepo := repository.NewBetaFeedbackRepository(db.GetCollection("beta_feedback_sessions"))
	apiKeyRepo := repository.NewAPIKeyRepository(db.GetCollection("api_keys"))
	activityRepo := repository.NewActivityRepository(db.GetCollection("activities"))
	auditRepo := repository.NewAdminAuditRepository(db.GetCollection("admin_audit_logs"))
	usageRepo := repository.NewUsageRepository(db.GetCollection("usage"))              // Add usage repository
	subRepo := repository.NewSubscriptionRepository(db.GetCollection("subscriptions")) // Add subscription repository
	paymentEventRepo := repository.NewPaymentEventRepository(db.GetCollection("payment_events"))
	planRepo := repository.NewPlanRepository(db.GetCollection("plans"))                // Add plan repository
	runtimeLogRepo := repository.NewRuntimeLogRepository(
		cfg.Logs.AccessPath,
		cfg.Logs.ErrorPath,
		cfg.Logs.NginxAccessPath,
		cfg.Logs.NginxErrorPath,
	)

	// Initialize services
	userService := services.NewUserService(userRepo, creditsRepo, activityRepo)
	creditsService := services.NewCreditsService(creditsRepo, userRepo)
	tokenService := services.NewCreditTokenService(tokenRepo, creditsRepo)
	betaKeyService := services.NewBetaKeyService(betaKeyRepo, userService)
	guestPassService := services.NewGuestPassService(guestPassRepo, creditsRepo)
	betaFeedbackService := services.NewBetaFeedbackService(betaFeedbackRepo, creditsService)
	apiKeyService := services.NewAPIKeyService(apiKeyRepo, userRepo)
	usageService := services.NewUsageService(usageRepo) // Add usage service
	planService := services.NewPlanService(planRepo)    // Add plan service
	emailService := services.NewEmailService(cfg.Email.FromEmail, cfg.Email.FromName, cfg.Email.Password)
	subService := services.NewSubscriptionService(subRepo, paymentEventRepo, creditsService, userService, planService, emailService, cfg.Razorpay.KeyID, cfg.Razorpay.KeySecret, cfg.Razorpay.WebhookSecret)
	adminService := services.NewAdminService(auditRepo, runtimeLogRepo, userService, tokenService, planService, usageService, subService)

	// Initialize API services
	qrAPIService := services.NewQRMaskingAPIService()
	qrExtractionAPIService := services.NewQRExtractionAPIService()
	idCroppingAPIService := services.NewIDCroppingAPIService()
	signatureAPIService := services.NewSignatureVerificationAPIService()
	faceDetectionAPIService := services.NewFaceDetectionAPIService()
	faceVerificationAPIService := services.NewFaceVerificationAPIService()

	log.Println("🔧 Using real API services")

	// Verify critical services are initialized
	if userService == nil {
		log.Fatal("❌ userService is nil")
	}
	if creditsService == nil {
		log.Fatal("❌ creditsService is nil")
	}
	if tokenService == nil {
		log.Fatal("❌ tokenService is nil")
	}
	if apiKeyService == nil {
		log.Fatal("❌ apiKeyService is nil")
	}
	if usageService == nil {
		log.Fatal("❌ usageService is nil")
	}
	if faceDetectionAPIService == nil {
		log.Fatal("❌ faceDetectionAPIService is nil")
	}

	log.Println("✅ All services initialized successfully")

	// Initialize handlers (only SignatureVerification has usage tracking implemented)
	handlers := &routes.Handlers{
		Health:  handlers.NewHealthHandler(),
		Admin:   handlers.NewAdminHandler(adminService),
		User:    handlers.NewUserHandler(userService, adminService),
		Credits: handlers.NewCreditsHandler(creditsService, userService, adminService),
		Token:   handlers.NewTokenHandler(tokenService, creditsService, userService, adminService),
		APIKey:  handlers.NewAPIKeyHandler(apiKeyService, userService),
		BetaKey: handlers.NewBetaKeyHandler(betaKeyService, adminService),
		BetaFeedback: handlers.NewBetaFeedbackHandler(betaFeedbackService, userService, adminService),
		GuestPass:    handlers.NewGuestPassHandler(guestPassService, adminService),
		// These handlers don't have usage tracking yet - using original constructors
		QRMasking:    handlers.NewQRMaskingHandler(creditsService, userService, qrAPIService, usageService),
		QRExtraction: handlers.NewQRExtractionHandler(creditsService, userService, qrExtractionAPIService, usageService),
		IDCropping:   handlers.NewIDCroppingHandler(creditsService, userService, idCroppingAPIService, usageService),
		// SignatureVerification has usage tracking implemented
		SignatureVerification: handlers.NewSignatureVerificationHandler(creditsService, userService, signatureAPIService, betaKeyService, betaFeedbackService, usageService),
		// These handlers don't have usage tracking yet - using original constructors
		FaceDetect:   handlers.NewFaceDetectionHandler(creditsService, userService, faceDetectionAPIService, usageService),
		FaceVerify:   handlers.NewFaceVerificationHandler(creditsService, userService, faceVerificationAPIService, usageService),
		Debug:        handlers.NewDebugHandler(),
		Usage:        handlers.NewUsageHandler(usageService),                    // Usage handler for admin endpoints
		Subscription: handlers.NewSubscriptionHandler(subService, adminService), // Add subscription handler
		Plan:         handlers.NewPlanHandler(planService, adminService),        // Add plan handler
	}

	// Verify handlers are initialized
	if handlers.FaceDetect == nil {
		log.Fatal("❌ FaceDetect handler is nil")
	}
	if handlers.Token == nil {
		log.Fatal("❌ Token handler is nil")
	}
	if handlers.APIKey == nil {
		log.Fatal("❌ APIKey handler is nil")
	}
	if handlers.Usage == nil {
		log.Fatal("❌ Usage handler is nil")
	}

	log.Println("✅ All handlers initialized successfully")

	services := &routes.Services{
		APIKeyService:    apiKeyService,
		GuestPassService: guestPassService,
		UsageService:     usageService, // Add usage service to routes
		UserRepo:         userRepo,
	}
	// Setup routes
	router := routes.SetupRoutes(handlers, services)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("🚀 Server starting on %s", server.Addr)
		log.Println("📋 Available endpoints:")
		log.Println("  GET  / - Health check")
		log.Println("  GET  /health - Health check")
		log.Println("  POST /api/v1/register - Register new user")
		log.Println("  POST /api/v1/credits/deduct - Deduct credits from user")
		log.Println("  POST /api/v1/credits/add - Add credits to user")
		log.Println("  GET  /api/v1/credits/balance - Get user's credit balance (requires Bearer token)")
		log.Println("  POST /api/v1/tokens/generate - Generate credit tokens (requires Bearer token)")
		log.Println("  POST /api/v1/tokens/redeem - Redeem credit tokens (requires Bearer token)")
		log.Println("  GET  /api/v1/tokens/my-tokens - Get user's generated tokens (requires Bearer token)")

		// API Key endpoints
		log.Println("  POST /api/v1/api-keys - Create new API key (requires Bearer token)")
		log.Println("  GET  /api/v1/api-keys - List user's API keys (requires Bearer token)")
		log.Println("  PUT  /api/v1/api-keys/{keyId} - Update API key (requires Bearer token)")
		log.Println("  DELETE /api/v1/api-keys/{keyId} - Revoke API key (requires Bearer token)")
		log.Println("  GET  /api/v1/api-keys/stats - Get API key statistics (requires Bearer token)")

		// Usage tracking endpoints (Admin only)
		log.Println("  GET  /api/v1/admin/usage/global - Get global usage statistics (Admin only)")
		log.Println("  GET  /api/v1/admin/usage/users - Get per-user usage statistics (Admin only)")
		log.Println("  GET  /api/v1/admin/usage/services - Get service-user usage statistics (Admin only)")
		log.Println("  GET  /api/v1/admin/usage/user/{userId}/history - Get user usage history (Admin only)")
		log.Println("  GET  /api/v1/admin/usage/service/{serviceName}/history - Get service usage history (Admin only)")

		log.Println("  POST /api/v1/qr-masking - Process QR masking (requires Bearer token or API key) [NO USAGE TRACKING]")
		log.Println("  POST /api/v1/qr-extraction - Process QR extraction (requires Bearer token or API key) [NO USAGE TRACKING]")
		log.Println("  POST /api/v1/id-cropping - Process ID cropping (requires Bearer token or API key) [NO USAGE TRACKING]")
		log.Println("  POST /api/v1/signature-verification - Process signature verification (requires Bearer token or API key) [WITH USAGE TRACKING]")
		log.Println("  POST /api/v1/face-detect - Process face detection (requires Bearer token or API key) [NO USAGE TRACKING]")
		log.Println("  POST /api/v1/face-verification - Process face verification (requires Bearer token or API key) [NO USAGE TRACKING]")

		// Subscription endpoints
		log.Println("  POST /api/v1/subscription/create-order - Create Razorpay order (requires Bearer token)")
		log.Println("  POST /api/v1/subscription/verify-payment - Verify Razorpay payment (requires Bearer token)")
		log.Println("  GET  /api/v1/subscription/status - Get user subscription status (requires Bearer token)")
		log.Println("  POST /api/v1/subscription/webhook - Razorpay webhook handler (Public)")
		log.Println("✅ CORS configured with allowed origins")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Server is shutting down...")

	// Gracefully shutdown the server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("❌ Server forced to shutdown: %v", err)
	}

	log.Println("✅ Server exited")
}
