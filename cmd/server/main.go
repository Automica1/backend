// Updated cmd/server/main.go
package main

import (
	"context"
	"fmt"
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

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func setupLogger() (*zap.Logger, error) {
	// Create logger configuration
	config := zap.NewProductionConfig()
	
	// Set log level based on environment
	if os.Getenv("ENV") == "development" {
		config = zap.NewDevelopmentConfig()
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	// Customize encoder config
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.MessageKey = "message"

	// Build logger
	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return logger, nil
}

func main() {
	// Setup logger
	logger, err := setupLogger()
	if err != nil {
		fmt.Printf("❌ Failed to setup logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() // Flush any buffered log entries

	// Replace the global logger
	zap.ReplaceGlobals(logger)

	logger.Info("🚀 Starting Chi-Mongo Backend Server")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}
	logger.Info("✅ Configuration loaded successfully")

	// Initialize database
	db, err := database.NewMongoDB(cfg)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			logger.Error("Error closing database connection", zap.Error(err))
		}
	}()

	logger.Info("✅ Successfully connected to MongoDB")

	// Initialize repositories
	userRepo := repository.NewUserRepository(db.GetCollection("users"))
	creditsRepo := repository.NewCreditsRepository(db.GetCollection("credits"))
	tokenRepo := repository.NewTokenRepository(db.GetCollection("tokens"))
	apiKeyRepo := repository.NewAPIKeyRepository(db.GetCollection("api_keys"))
	activityRepo := repository.NewActivityRepository(db.GetCollection("activities"))

	logger.Info("✅ All repositories initialized successfully")

	// Initialize services
	userService := services.NewUserService(userRepo, creditsRepo, activityRepo)
	creditsService := services.NewCreditsService(creditsRepo, userRepo)
	tokenService := services.NewCreditTokenService(tokenRepo, creditsRepo)
	apiKeyService := services.NewAPIKeyService(apiKeyRepo, userRepo)
	
	// Initialize API services
	qrAPIService := services.NewQRMaskingAPIService()
	qrExtractionAPIService := services.NewQRExtractionAPIService()
	idCroppingAPIService := services.NewIDCroppingAPIService()
	signatureAPIService := services.NewSignatureVerificationAPIService()
	faceDetectionAPIService := services.NewFaceDetectionAPIService()
	faceVerificationAPIService := services.NewFaceVerificationAPIService()
	
	logger.Info("🔧 Using real API services")

	// Verify critical services are initialized
	if userService == nil {
		logger.Fatal("userService is nil")
	}
	if creditsService == nil {
		logger.Fatal("creditsService is nil")
	}
	if tokenService == nil {
		logger.Fatal("tokenService is nil")
	}
	if apiKeyService == nil {
		logger.Fatal("apiKeyService is nil")
	}
	if faceDetectionAPIService == nil {
		logger.Fatal("faceDetectionAPIService is nil")
	}

	logger.Info("✅ All services initialized successfully")

	// Initialize handlers
	handlers := &routes.Handlers{
		Health:                handlers.NewHealthHandler(),
		User:                  handlers.NewUserHandler(userService),
		Credits:               handlers.NewCreditsHandler(creditsService, userService),
		Token:                 handlers.NewTokenHandler(tokenService, creditsService, userService),
		APIKey:                handlers.NewAPIKeyHandler(apiKeyService, userService),
		QRMasking:             handlers.NewQRMaskingHandler(creditsService, userService, qrAPIService),
		QRExtraction:          handlers.NewQRExtractionHandler(creditsService, userService, qrExtractionAPIService),
		IDCropping:            handlers.NewIDCroppingHandler(creditsService, userService, idCroppingAPIService),
		SignatureVerification: handlers.NewSignatureVerificationHandler(creditsService, userService, signatureAPIService),
		FaceDetect:            handlers.NewFaceDetectionHandler(creditsService, userService, faceDetectionAPIService), 
		FaceVerify:            handlers.NewFaceVerificationHandler(creditsService, userService, faceVerificationAPIService),
		Debug:                 handlers.NewDebugHandler(),
	}

	// Verify handlers are initialized
	if handlers.FaceDetect == nil {
		logger.Fatal("FaceDetect handler is nil")
	}
	if handlers.Token == nil {
		logger.Fatal("Token handler is nil")
	}
	if handlers.APIKey == nil {
		logger.Fatal("APIKey handler is nil")
	}

	logger.Info("✅ All handlers initialized successfully")

	services := &routes.Services{
        APIKeyService: apiKeyService,
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
		logger.Info("🚀 Server starting", 
			zap.String("address", server.Addr),
			zap.Duration("read_timeout", server.ReadTimeout),
			zap.Duration("write_timeout", server.WriteTimeout),
			zap.Duration("idle_timeout", server.IdleTimeout),
		)
		
		// Log available endpoints
		endpoints := []string{
			"GET  / - Health check",
			"GET  /health - Health check", 
			"GET  /debug/token - Debug token data (NO AUTH REQUIRED)",
			"POST /api/v1/register - Register new user",
			"POST /api/v1/credits/deduct - Deduct credits from user",
			"POST /api/v1/credits/add - Add credits to user",
			"GET  /api/v1/credits/balance - Get user's credit balance (requires Bearer token)",
			"POST /api/v1/tokens/generate - Generate credit tokens (requires Bearer token)",
			"POST /api/v1/tokens/redeem - Redeem credit tokens (requires Bearer token)",
			"GET  /api/v1/tokens/my-tokens - Get user's generated tokens (requires Bearer token)",
			"POST /api/v1/api-keys - Create new API key (requires Bearer token)",
			"GET  /api/v1/api-keys - List user's API keys (requires Bearer token)",
			"PUT  /api/v1/api-keys/{keyId} - Update API key (requires Bearer token)",
			"DELETE /api/v1/api-keys/{keyId} - Revoke API key (requires Bearer token)",
			"GET  /api/v1/api-keys/stats - Get API key statistics (requires Bearer token)",
			"POST /api/v1/qr-masking - Process QR masking (requires Bearer token or API key)",
			"POST /api/v1/qr-extraction - Process QR extraction (requires Bearer token or API key)",
			"POST /api/v1/id-cropping - Process ID cropping (requires Bearer token or API key)",
			"POST /api/v1/signature-verification - Process signature verification (requires Bearer token or API key)",
			"POST /api/v1/face-detect - Process face detection (requires Bearer token or API key)",
			"POST /api/v1/face-verification - Process face verification (requires Bearer token or API key)",
		}

		logger.Info("📋 Available endpoints", zap.Strings("endpoints", endpoints))
		logger.Info("✅ CORS enabled for all origins")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("🛑 Server is shutting down...")

	// Gracefully shutdown the server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("✅ Server exited gracefully")
}