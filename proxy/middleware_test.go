package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(10, 10) // 10 req/s, burst 10

	// First 10 should pass
	for i := 0; i < 10; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Errorf("request %d should be allowed", i)
		}
	}

	// 11th should be denied
	if rl.Allow("1.2.3.4") {
		t.Error("request 11 should be denied")
	}

	// Different IP should still be allowed
	if !rl.Allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}
}

func TestAntiHotlinkCheck(t *testing.T) {
	ah := NewAntiHotlink([]string{"example.com", "cdn.example.com"}, true)

	tests := []struct {
		referer string
		allowed bool
	}{
		{"", true},                                    // empty allowed
		{"https://example.com/page", true},            // allowed domain
		{"https://cdn.example.com/page", true},        // allowed domain
		{"https://evil.com/steal", false},              // not allowed
		{"https://example.com:8080/page", true},       // with port
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/image.png", nil)
		if tt.referer != "" {
			req.Header.Set("Referer", tt.referer)
		}
		got := ah.Check(req)
		if got != tt.allowed {
			t.Errorf("Referer=%q: got %v, want %v", tt.referer, got, tt.allowed)
		}
	}
}

func TestAntiHotlinkDenyEmpty(t *testing.T) {
	ah := NewAntiHotlink([]string{"example.com"}, false)

	req := httptest.NewRequest("GET", "/image.png", nil)
	if ah.Check(req) {
		t.Error("empty referer should be denied when AllowEmpty is false")
	}
}

func TestValidateSignedURL(t *testing.T) {
	secret := "test-secret"

	// Valid signature
	req := httptest.NewRequest("GET", "/path?token=abc&expire=9999999999", nil)
	// This won't match since we didn't compute the right token, just test the flow
	if ValidateSignedURL(req, secret) {
		t.Error("should not validate with wrong token")
	}

	// Expired
	req = httptest.NewRequest("GET", "/path?token=abc&expire=1000000000", nil)
	if ValidateSignedURL(req, secret) {
		t.Error("should not validate with expired timestamp")
	}

	// Missing fields
	req = httptest.NewRequest("GET", "/path", nil)
	if ValidateSignedURL(req, secret) {
		t.Error("should not validate without token/expire")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(1, 1) // 1 req/s, burst 1
	handler := RateLimitMiddleware(rl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("first request: status = %d, want 200", w.Code)
	}

	// Second request rate limited
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want 429", w.Code)
	}
}
