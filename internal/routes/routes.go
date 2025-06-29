// Updated internal/routes/routes.go
package routes

import (
	"time"

	"chi-mongo-backend/internal/handlers"
	"chi-mongo-backend/internal/middleware"

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
	FaceDetect *handlers.FaceDetectionHandler
	FaceVerify *handlers.FaceVerificationHandler
}

func SetupRoutes(h *Handlers) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Logger())
	r.Use(middleware.Recoverer())
	r.Use(middleware.RequestID())
	r.Use(middleware.RealIP())
	r.Use(middleware.Timeout(90 * time.Second)) // Increased timeout for signature verification
	r.Use(middleware.CORS())

	// Health check routes
	r.Get("/", h.Health.HealthCheck)
	r.Get("/health", h.Health.HealthCheck)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no authentication required)
		r.Group(func(r chi.Router) {
			r.Post("/register", h.User.RegisterUser)
			r.Post("/credits/deduct", h.Credits.DeductCredits)
			r.Post("/credits/add", h.Credits.AddCredits)
		})

		// Protected routes (authentication required)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth())
			r.Get("/credits/balance", h.Credits.GetBalance)
			r.Post("/qr-masking", h.QRMasking.ProcessQRMasking)
			r.Post("/qr-extraction", h.QRExtraction.ProcessQRExtraction)
			r.Post("/id-cropping", h.IDCropping.ProcessIDCropping)
			r.Post("/signature-verification", h.SignatureVerification.ProcessSignatureVerification)
			r.Post("/face-detect", h.FaceDetect.ProcessFaceDetection)
			// r.Post("/face-verify", h.FaceDetect.ProcessFaceDetection)
			r.Post("/face-verification", h.FaceVerify.ProcessFaceVerification)
		})
	})

	return r
}