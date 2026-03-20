package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// RateLimiter implements a simple token bucket rate limiter per IP.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int           // tokens per second
	burst   int           // max tokens
	cleanup time.Duration // cleanup interval
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter(rate, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		cleanup: 5 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[ip]
	if !exists {
		rl.buckets[ip] = &bucket{tokens: float64(rl.burst) - 1, lastCheck: now}
		return true
	}

	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * float64(rl.rate)
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastCheck = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.cleanup)
		for ip, b := range rl.buckets {
			if b.lastCheck.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware wraps an http.Handler with rate limiting.
func RateLimitMiddleware(rl *RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.Allow(ip) {
			cdnlog.Debug("rate limited", "ip", ip)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AntiHotlink checks Referer header against allowed domains.
type AntiHotlink struct {
	AllowedDomains map[string]bool
	AllowEmpty     bool // allow requests with no Referer
}

// NewAntiHotlink creates an anti-hotlink checker.
func NewAntiHotlink(domains []string, allowEmpty bool) *AntiHotlink {
	allowed := make(map[string]bool)
	for _, d := range domains {
		allowed[strings.ToLower(d)] = true
	}
	return &AntiHotlink{AllowedDomains: allowed, AllowEmpty: allowEmpty}
}

// Check returns true if the request's Referer is allowed.
func (ah *AntiHotlink) Check(r *http.Request) bool {
	referer := r.Header.Get("Referer")
	if referer == "" {
		return ah.AllowEmpty
	}

	// Extract host from Referer
	// Format: scheme://host/path
	idx := strings.Index(referer, "://")
	if idx == -1 {
		return false
	}
	rest := referer[idx+3:]
	host := rest
	if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
		host = rest[:slashIdx]
	}
	// Remove port
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		host = host[:colonIdx]
	}

	return ah.AllowedDomains[strings.ToLower(host)]
}

// AntiHotlinkMiddleware wraps a handler with Referer checking.
func AntiHotlinkMiddleware(ah *AntiHotlink, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ah.Check(r) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateSignedURL checks if a URL has a valid signature token.
// URL format: /path?token={md5}&expire={timestamp}
func ValidateSignedURL(r *http.Request, secret string) bool {
	token := r.URL.Query().Get("token")
	expireStr := r.URL.Query().Get("expire")
	if token == "" || expireStr == "" {
		return false
	}

	expire, err := strconv.ParseInt(expireStr, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expire {
		return false
	}

	// Compute expected signature: HMAC-SHA256(secret, path+expire)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(r.URL.Path + expireStr))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(token), []byte(expected))
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
