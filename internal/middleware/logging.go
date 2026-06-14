// internal/middleware/logging.go
package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type accessLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Route     string `json:"route,omitempty"`
	Status    int    `json:"status"`
	Bytes     int    `json:"bytes"`
	Duration  int64  `json:"duration_ms"`
	RemoteIP  string `json:"remote_ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Email     string `json:"email,omitempty"`
	IsAdmin   bool   `json:"is_admin,omitempty"`
}

type accessResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (rw *accessResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *accessResponseWriter) Write(data []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	n, err := rw.ResponseWriter.Write(data)
	rw.bytes += n
	return n, err
}

func Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &accessResponseWriter{ResponseWriter: w}

			next.ServeHTTP(ww, r)

			requestID := middleware.GetReqID(r.Context())
			routePattern := ""
			if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
				routePattern = routeCtx.RoutePattern()
			}

			email, _ := GetEmailFromContext(r.Context())
			isAdmin := IsAdminFromContext(r.Context())

			entry := accessLogEntry{
				Timestamp: start.UTC().Format(time.RFC3339Nano),
				Level:     "info",
				Message:   "http_request",
				RequestID: requestID,
				Method:    r.Method,
				Path:      r.URL.Path,
				Route:     routePattern,
				Status:    ww.statusCode,
				Bytes:     ww.bytes,
				Duration:  time.Since(start).Milliseconds(),
				RemoteIP:  r.RemoteAddr,
				UserAgent: r.UserAgent(),
				Email:     email,
				IsAdmin:   isAdmin,
			}

			payload, err := json.Marshal(entry)
			if err != nil {
				log.Printf("failed to encode access log: %v", err)
				return
			}

			_, _ = fmt.Fprintln(os.Stdout, string(payload))
		})
	}
}

// RequestID adds a unique request ID to each request
func RequestID() func(http.Handler) http.Handler {
	return middleware.RequestID
}

// Timeout adds a timeout to requests
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return middleware.Timeout(timeout)
}

// Recoverer recovers from panics and returns a 500 error
func Recoverer() func(http.Handler) http.Handler {
	return middleware.Recoverer
}

// RealIP gets the real IP from various headers
func RealIP() func(http.Handler) http.Handler {
	return middleware.RealIP
}
