// internal/middleware/cors.go
package middleware

import (
	"net/http"
	"os"
	"strings"
)

func CORS() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" && isAllowedCORSOrigin(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Max-Age", "86400")

			// Handle preflight OPTIONS request
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Continue to the next handler
			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedCORSOrigin(origin string) bool {
	allowed := corsAllowedOrigins()
	for _, candidate := range allowed {
		if candidate == origin {
			return true
		}
	}
	return false
}

func corsAllowedOrigins() []string {
	if value := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS")); value != "" {
		parts := strings.Split(value, ",")
		allowed := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				allowed = append(allowed, trimmed)
			}
		}
		return allowed
	}

	return []string{
		"https://automica.ai",
		"https://dev.automica.ai",
		"http://localhost:3000",
		"http://127.0.0.1:3000",
	}
}
