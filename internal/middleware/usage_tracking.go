// 4. Create usage tracking middleware
// internal/middleware/usage_tracking.go
package middleware

import (
	"context"
	"net/http"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
)

// UsageTracker middleware for tracking service usage
func UsageTracker(usageService services.UsageService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Start tracking time
			startTime := time.Now()
			
			// Create response writer wrapper to capture status
			ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			
			// Store start time in context for handlers to use
			ctx := context.WithValue(r.Context(), "start_time", startTime)
			ctx = context.WithValue(ctx, "usage_service", usageService)
			
			// Call next handler
			next.ServeHTTP(ww, r.WithContext(ctx))
		})
	}
}

// Helper function to track usage from handlers
func TrackServiceUsage(ctx context.Context, req *models.UsageTrackingRequest) {
	usageService, ok := ctx.Value("usage_service").(services.UsageService)
	if !ok {
		return // Skip tracking if service not available
	}
	
	// Track usage asynchronously to not block the response
	go func() {
		// Create a new context for the async operation
		trackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		if err := usageService.TrackUsage(trackCtx, req); err != nil {
			// Log error but don't fail the request
			// In production, you might want to use a proper logger
			// log.Printf("Failed to track usage: %v", err)
		}
	}()
}

// responseWriter wrapper to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}