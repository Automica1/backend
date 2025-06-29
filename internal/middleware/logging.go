// internal/middleware/logging.go
package middleware

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func Logger() func(http.Handler) http.Handler {
	return middleware.RequestLogger(&middleware.DefaultLogFormatter{
		Logger:  log.New(log.Writer(), "", log.LstdFlags),
		NoColor: false,
	})
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