package proxy

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// DDoSConfig configures DDoS mitigation parameters.
type DDoSConfig struct {
	MaxConnsPerIP    int           // max concurrent connections per IP
	RequestsPerSec   int           // max requests per second per IP (sliding window)
	WindowSize       time.Duration // sliding window size
	AutoBanThreshold int           // auto-ban after this many violations
	AutoBanDuration  time.Duration // auto-ban duration
	ChallengePath    string        // path to serve JS challenge
}

// DefaultDDoSConfig returns sensible defaults.
func DefaultDDoSConfig() DDoSConfig {
	return DDoSConfig{
		MaxConnsPerIP:    100,
		RequestsPerSec:   50,
		WindowSize:       10 * time.Second,
		AutoBanThreshold: 5,
		AutoBanDuration:  5 * time.Minute,
		ChallengePath:    "/__edgeflow_challenge",
	}
}

// DDoSProtector implements DDoS mitigation.
type DDoSProtector struct {
	cfg        DDoSConfig
	mu         sync.Mutex
	connCounts map[string]*atomic.Int64 // IP -> active connections
	windows    map[string]*slidingWindow
	banned     map[string]time.Time // IP -> ban expiry
	violations map[string]int       // IP -> violation count
	stopCh     chan struct{}
}

type slidingWindow struct {
	counts []int64
	index  int
	total  int64
}

// NewDDoSProtector creates a DDoS protector.
func NewDDoSProtector(cfg DDoSConfig) *DDoSProtector {
	d := &DDoSProtector{
		cfg:        cfg,
		connCounts: make(map[string]*atomic.Int64),
		windows:    make(map[string]*slidingWindow),
		banned:     make(map[string]time.Time),
		violations: make(map[string]int),
		stopCh:     make(chan struct{}),
	}
	go d.cleanupLoop()
	return d
}

// CheckRequest returns true if the request should be allowed.
func (d *DDoSProtector) CheckRequest(ip string) (allowed bool, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check ban list
	if expiry, banned := d.banned[ip]; banned {
		if time.Now().Before(expiry) {
			return false, "banned"
		}
		delete(d.banned, ip)
		delete(d.violations, ip)
	}

	// Check connection count
	if counter, exists := d.connCounts[ip]; exists {
		if counter.Load() > int64(d.cfg.MaxConnsPerIP) {
			d.recordViolation(ip)
			return false, "too_many_connections"
		}
	}

	// Check request rate (simple counter per window)
	w, exists := d.windows[ip]
	if !exists {
		w = &slidingWindow{}
		d.windows[ip] = w
	}
	w.total++
	if w.total > int64(d.cfg.RequestsPerSec)*int64(d.cfg.WindowSize.Seconds()) {
		d.recordViolation(ip)
		return false, "rate_exceeded"
	}

	return true, ""
}

// TrackConnection tracks an active connection for an IP.
func (d *DDoSProtector) TrackConnection(ip string) func() {
	d.mu.Lock()
	counter, exists := d.connCounts[ip]
	if !exists {
		counter = &atomic.Int64{}
		d.connCounts[ip] = counter
	}
	d.mu.Unlock()

	counter.Add(1)
	return func() {
		counter.Add(-1)
	}
}

func (d *DDoSProtector) recordViolation(ip string) {
	d.violations[ip]++
	if d.violations[ip] >= d.cfg.AutoBanThreshold {
		d.banned[ip] = time.Now().Add(d.cfg.AutoBanDuration)
		cdnlog.Warn("auto-banned IP", "ip", ip, "duration", d.cfg.AutoBanDuration)
	}
}

// IsBanned checks if an IP is currently banned.
func (d *DDoSProtector) IsBanned(ip string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if expiry, banned := d.banned[ip]; banned {
		if time.Now().Before(expiry) {
			return true
		}
		delete(d.banned, ip)
	}
	return false
}

func (d *DDoSProtector) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.cleanup()
		}
	}
}

func (d *DDoSProtector) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	// Clean expired bans
	for ip, expiry := range d.banned {
		if now.After(expiry) {
			delete(d.banned, ip)
			delete(d.violations, ip)
		}
	}
	// Clean idle connection counters
	for ip, counter := range d.connCounts {
		if counter.Load() <= 0 {
			delete(d.connCounts, ip)
		}
	}
	// Reset windows
	d.windows = make(map[string]*slidingWindow)
}

// Stop stops the DDoS protector.
func (d *DDoSProtector) Stop() {
	close(d.stopCh)
}

// DDoSMiddleware wraps a handler with DDoS protection.
func DDoSMiddleware(d *DDoSProtector, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		allowed, reason := d.CheckRequest(ip)
		if !allowed {
			cdnlog.Debug("DDoS blocked", "ip", ip, "reason", reason)
			if reason == "banned" {
				http.Error(w, "Forbidden", http.StatusForbidden)
			} else {
				w.Header().Set("Retry-After", "5")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ChallengePageHTML returns a simple JS challenge page.
func ChallengePageHTML() string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Checking your browser</title></head>
<body>
<p>Please wait while we verify your browser...</p>
<script>
document.cookie = "__ef_check=" + (Date.now() ^ 0x5A3B) + "; path=/; max-age=3600";
setTimeout(function(){ location.reload(); }, 1500);
</script>
</body>
</html>`)
}
