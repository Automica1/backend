// cmd/server/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"chi-mongo-backend/internal/config"
	"chi-mongo-backend/internal/database"
	"chi-mongo-backend/internal/handlers"
	"chi-mongo-backend/internal/repository"
	"chi-mongo-backend/internal/routes"
	"chi-mongo-backend/internal/services"
)

func initLogger(env string) *zap.Logger {
	var config zap.Config
	
	if env == "production" {
		config = zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	} else {
		config = zap.NewDevelopmentConfig()
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	
	// Customize time format
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	
	logger, err := config.Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
	
	return logger
}

func main() {
	// Initialize logger first
	logger := initLogger(os.Getenv("ENV"))
	defer logger.Sync() // Flush any buffered log entries
	
	// Replace global logger
	zap.ReplaceGlobals(logger)

	logger.Info("Starting chi-mongo-backend server")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	logger.Info("Configuration loaded successfully",
		zap.String("host", cfg.Server.Host),
		zap.String("port", cfg.Server.Port))

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

	logger.Info("Successfully connected to MongoDB")

	// Initialize repositories
	logger.Debug("Initializing repositories")
	userRepo := repository.NewUserRepository(db.GetCollection("users"))
	creditsRepo := repository.NewCreditsRepository(db.GetCollection("credits"))
	tokenRepo := repository.NewTokenRepository(db.GetCollection("tokens"))
	apiKeyRepo := repository.NewAPIKeyRepository(db.GetCollection("api_keys"))
	activityRepo := repository.NewActivityRepository(db.GetCollection("activities"))
	usageRepo := repository.NewUsageRepository(db.GetCollection("usage"))

	logger.Info("All repositories initialized successfully")

	// Initialize services
	logger.Debug("Initializing services")
	userService := services.NewUserService(userRepo, creditsRepo, activityRepo)
	creditsService := services.NewCreditsService(creditsRepo, userRepo)
	tokenService := services.NewCreditTokenService(tokenRepo, creditsRepo)
	apiKeyService := services.NewAPIKeyService(apiKeyRepo, userRepo)
	usageService := services.NewUsageService(usageRepo)
	
	// Initialize API services
	qrAPIService := services.NewQRMaskingAPIService()
	qrExtractionAPIService := services.NewQRExtractionAPIService()
	idCroppingAPIService := services.NewIDCroppingAPIService()
	signatureAPIService := services.NewSignatureVerificationAPIService()
	faceDetectionAPIService := services.NewFaceDetectionAPIService()
	faceVerificationAPIService := services.NewFaceVerificationAPIService()
	
	logger.Info("Using real API services")

	// Verify critical services are initialized
	services := []interface{}{
		userService, creditsService, tokenService, apiKeyService, usageService, faceDetectionAPIService,
	}
	serviceNames := []string{
		"userService", "creditsService", "tokenService", "apiKeyService", "usageService", "faceDetectionAPIService",
	}

	for i, service := range services {
		if service == nil {
			logger.Fatal("Service is nil", zap.String("service", serviceNames[i]))
		}
	}

	logger.Info("All services initialized successfully")

	// Initialize handlers
	logger.Debug("Initializing handlers")
	handlers := &routes.Handlers{
		Health:                handlers.NewHealthHandler(),
		User:                  handlers.NewUserHandler(userService),
		Credits:               handlers.NewCreditsHandler(creditsService, userService),
		Token:                 handlers.NewTokenHandler(tokenService, creditsService, userService),
		APIKey:                handlers.NewAPIKeyHandler(apiKeyService, userService),
		QRMasking:             handlers.NewQRMaskingHandler(creditsService, userService, qrAPIService),
		QRExtraction:          handlers.NewQRExtractionHandler(creditsService, userService, qrExtractionAPIService),
		IDCropping:            handlers.NewIDCroppingHandler(creditsService, userService, idCroppingAPIService),
		SignatureVerification: handlers.NewSignatureVerificationHandler(creditsService, userService, signatureAPIService, usageService),
		FaceDetect:            handlers.NewFaceDetectionHandler(creditsService, userService, faceDetectionAPIService),
		FaceVerify:            handlers.NewFaceVerificationHandler(creditsService, userService, faceVerificationAPIService),
		Debug:                 handlers.NewDebugHandler(),
		Usage:                 handlers.NewUsageHandler(usageService),
	}

	// Verify handlers are initialized
	criticalHandlers := []interface{}{
		handlers.FaceDetect, handlers.Token, handlers.APIKey, handlers.Usage,
	}
	handlerNames := []string{
		"FaceDetect", "Token", "APIKey", "Usage",
	}

	for i, handler := range criticalHandlers {
		if handler == nil {
			logger.Fatal("Handler is nil", zap.String("handler", handlerNames[i]))
		}
	}

	logger.Info("All handlers initialized successfully")

	servicesStruct := &routes.Services{
        APIKeyService: apiKeyService,
		UsageService:  usageService,
    }
	
	// Setup routes
	logger.Debug("Setting up routes")
	router := routes.SetupRoutes(handlers, servicesStruct)

	// Create HTTP server
	serverAddr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("Starting HTTP server",
			zap.String("address", serverAddr),
			zap.Duration("read_timeout", 30*time.Second),
			zap.Duration("write_timeout", 30*time.Second),
			zap.Duration("idle_timeout", 60*time.Second))

		// Log available endpoints
		endpoints := []struct {
			method      string
			path        string
			description string
			auth        string
			tracking    string
		}{
			{"GET", "/", "Health check", "None", ""},
			{"GET", "/health", "Health check", "None", ""},
			{"GET", "/debug/token", "Debug token data", "None", ""},
			{"POST", "/api/v1/register", "Register new user", "None", ""},
			{"POST", "/api/v1/credits/deduct", "Deduct credits from user", "Bearer token", ""},
			{"POST", "/api/v1/credits/add", "Add credits to user", "Bearer token", ""},
			{"GET", "/api/v1/credits/balance", "Get user's credit balance", "Bearer token", ""},
			{"POST", "/api/v1/tokens/generate", "Generate credit tokens", "Bearer token", ""},
			{"POST", "/api/v1/tokens/redeem", "Redeem credit tokens", "Bearer token", ""},
			{"GET", "/api/v1/tokens/my-tokens", "Get user's generated tokens", "Bearer token", ""},
			{"POST", "/api/v1/api-keys", "Create new API key", "Bearer token", ""},
			{"GET", "/api/v1/api-keys", "List user's API keys", "Bearer token", ""},
			{"PUT", "/api/v1/api-keys/{keyId}", "Update API key", "Bearer token", ""},
			{"DELETE", "/api/v1/api-keys/{keyId}", "Revoke API key", "Bearer token", ""},
			{"GET", "/api/v1/api-keys/stats", "Get API key statistics", "Bearer token", ""},
			{"GET", "/api/v1/admin/usage/global", "Get global usage statistics", "Admin only", ""},
			{"GET", "/api/v1/admin/usage/users", "Get per-user usage statistics", "Admin only", ""},
			{"GET", "/api/v1/admin/usage/services", "Get service-user usage statistics", "Admin only", ""},
			{"GET", "/api/v1/admin/usage/user/{userId}/history", "Get user usage history", "Admin only", ""},
			{"GET", "/api/v1/admin/usage/service/{serviceName}/history", "Get service usage history", "Admin only", ""},
			{"POST", "/api/v1/qr-masking", "Process QR masking", "Bearer token or API key", "NO USAGE TRACKING"},
			{"POST", "/api/v1/qr-extraction", "Process QR extraction", "Bearer token or API key", "NO USAGE TRACKING"},
			{"POST", "/api/v1/id-cropping", "Process ID cropping", "Bearer token or API key", "NO USAGE TRACKING"},
			{"POST", "/api/v1/signature-verification", "Process signature verification", "Bearer token or API key", "WITH USAGE TRACKING"},
			{"POST", "/api/v1/face-detect", "Process face detection", "Bearer token or API key", "NO USAGE TRACKING"},
			{"POST", "/api/v1/face-verification", "Process face verification", "Bearer token or API key", "NO USAGE TRACKING"},
		}

		logger.Info("Available endpoints", zap.Int("count", len(endpoints)))
		for _, endpoint := range endpoints {
			fields := []zap.Field{
				zap.String("method", endpoint.method),
				zap.String("path", endpoint.path),
				zap.String("description", endpoint.description),
				zap.String("auth", endpoint.auth),
			}
			if endpoint.tracking != "" {
				fields = append(fields, zap.String("tracking", endpoint.tracking))
			}
			logger.Debug("Endpoint registered", fields...)
		}

		logger.Info("CORS enabled for all origins")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed to start", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Received shutdown signal, shutting down server gracefully")

	// Gracefully shutdown the server with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited gracefully")
}