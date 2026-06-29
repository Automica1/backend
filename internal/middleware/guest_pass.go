package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"chi-mongo-backend/internal/models"
	"chi-mongo-backend/internal/services"
	apperrors "chi-mongo-backend/pkg/errors"
	"chi-mongo-backend/pkg/utils"
)

const (
	GuestPassContextKey contextKey = "guest_pass"
)

type guestPassRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

var guestPassLimiter = &guestPassRateLimiter{
	attempts: make(map[string][]time.Time),
}

func (l *guestPassRateLimiter) allow(ip string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)
	history := l.attempts[ip]
	filtered := history[:0]
	for _, ts := range history {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= max {
		l.attempts[ip] = filtered
		return false
	}
	filtered = append(filtered, now)
	l.attempts[ip] = filtered
	return true
}

func GetGuestPassFromContext(ctx context.Context) (*models.GuestPass, bool) {
	pass, ok := ctx.Value(GuestPassContextKey).(*models.GuestPass)
	return pass, ok
}

func IsGuestPassAuth(ctx context.Context) bool {
	_, ok := GetGuestPassFromContext(ctx)
	return ok
}

func AuthOrAPIKeyOrGuestPass(apiKeyService services.APIKeyService, guestPassService services.GuestPassService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			guestPassHeader := strings.TrimSpace(r.Header.Get("X-Guest-Pass"))
			authHeader := r.Header.Get("Authorization")

			if guestPassHeader != "" && (authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ak_live_")) {
				clientIP := r.RemoteAddr
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					clientIP = strings.TrimSpace(strings.Split(xff, ",")[0])
				}
				if !guestPassLimiter.allow(clientIP, 10, time.Minute) {
					utils.SendErrorResponse(w, apperrors.NewAppError(
						apperrors.ErrBadRequest,
						http.StatusTooManyRequests,
						"too many guest pass attempts",
					))
					return
				}

				pass, err := guestPassService.ValidateKey(r.Context(), guestPassHeader)
				if err != nil {
					utils.SendErrorResponse(w, err)
					return
				}

				ctx := context.WithValue(r.Context(), GuestPassContextKey, pass)
				ctx = context.WithValue(ctx, UserIDContextKey, pass.WalletUserID)
				ctx = context.WithValue(ctx, "email", pass.WalletUserID)

				go func() {
					updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = guestPassService.RecordUsage(updateCtx, pass.KeyHash)
				}()

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			AuthOrAPIKey(apiKeyService)(next).ServeHTTP(w, r)
		})
	}
}

func ResolveAuthMethod(r *http.Request) string {
	if _, ok := GetGuestPassFromContext(r.Context()); ok {
		return "guest_pass"
	}
	if _, ok := GetAPIKeyFromContext(r.Context()); ok {
		return "api_key"
	}
	return "bearer_token"
}
