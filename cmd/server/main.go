// Updated cmd/server/main.go
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

	// Initialize services
	userService := services.NewUserService(userRepo, creditsRepo)
	creditsService := services.NewCreditsService(creditsRepo, userRepo)
	
	// Initialize API services
	qrAPIService := services.NewQRMaskingAPIService()
	qrExtractionAPIService := services.NewQRExtractionAPIService()
	idCroppingAPIService := services.NewIDCroppingAPIService()
	signatureAPIService := services.NewSignatureVerificationAPIService()
	faceDetectionAPIService := services.NewFaceDetectionAPIService() // Add face detection API service
	faceVerificationAPIService := services.NewFaceVerificationAPIService()
	
	log.Println("🔧 Using real API services")

	// Verify critical services are initialized
	if userService == nil {
		log.Fatal("❌ userService is nil")
	}
	if creditsService == nil {
		log.Fatal("❌ creditsService is nil")
	}
	if faceDetectionAPIService == nil {
		log.Fatal("❌ faceDetectionAPIService is nil")
	}

	log.Println("✅ All services initialized successfully")

	// Initialize handlers
	handlers := &routes.Handlers{
		Health:                handlers.NewHealthHandler(),
		User:                  handlers.NewUserHandler(userService),
		Credits:               handlers.NewCreditsHandler(creditsService, userService),
		QRMasking:             handlers.NewQRMaskingHandler(creditsService, userService, qrAPIService),
		QRExtraction:          handlers.NewQRExtractionHandler(creditsService, userService, qrExtractionAPIService),
		IDCropping:            handlers.NewIDCroppingHandler(creditsService, userService, idCroppingAPIService),
		SignatureVerification: handlers.NewSignatureVerificationHandler(creditsService, userService, signatureAPIService),
		FaceDetect:            handlers.NewFaceDetectionHandler(creditsService, userService, faceDetectionAPIService), 
		FaceVerify:          handlers.NewFaceVerificationHandler(creditsService, userService, faceVerificationAPIService), // Add face verification handler
		// Add face detection handler
	}

	// Verify handlers are initialized
	if handlers.FaceDetect == nil {
		log.Fatal("❌ FaceDetect handler is nil")
	}

	log.Println("✅ All handlers initialized successfully")

	// Setup routes
	router := routes.SetupRoutes(handlers)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,  // Increased for larger payloads
		WriteTimeout: 30 * time.Second,  // Increased for processing time
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
		log.Println("  POST /api/v1/qr-masking - Process QR masking (requires Bearer token)")
		log.Println("  POST /api/v1/qr-extraction - Process QR extraction (requires Bearer token)")
		log.Println("  POST /api/v1/id-cropping - Process ID cropping (requires Bearer token)")
		log.Println("  POST /api/v1/signature-verification - Process signature verification (requires Bearer token)")
		log.Println("  POST /api/v1/face-detect - Process face detection (requires Bearer token)")
		log.Println("✅ CORS enabled for all origins")

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