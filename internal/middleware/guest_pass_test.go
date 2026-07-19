package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGuestPassPublicRateLimitThrottlesPerIP(t *testing.T) {
	limited := GuestPassPublicRateLimit(3, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	send := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/guest-passes/validate", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 3; i++ {
		if code := send("10.0.0.1:1234"); code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i+1, code)
		}
	}
	if code := send("10.0.0.1:1234"); code != http.StatusTooManyRequests {
		t.Fatalf("over-limit request: status = %d, want 429", code)
	}

	// A different client IP must not be affected by the first IP's usage.
	if code := send("10.0.0.2:1234"); code != http.StatusOK {
		t.Fatalf("other IP: status = %d, want 200", code)
	}
}

func TestGuestPassPublicRateLimitUsesForwardedFor(t *testing.T) {
	limited := GuestPassPublicRateLimit(1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	send := func(forwardedFor string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/guest-passes/balance", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-For", forwardedFor)
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send("203.0.113.7"); code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", code)
	}
	if code := send("203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("second request same client: status = %d, want 429", code)
	}
	if code := send("203.0.113.8"); code != http.StatusOK {
		t.Fatalf("different forwarded client: status = %d, want 200", code)
	}
}
