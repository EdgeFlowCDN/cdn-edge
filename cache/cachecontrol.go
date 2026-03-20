package cache

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/EdgeFlowCDN/cdn-edge/config"
)

// Directives represents parsed Cache-Control directives.
type Directives struct {
	NoStore        bool
	NoCache        bool
	Private        bool
	Public         bool
	SMaxAge        int // seconds, -1 if not present
	MaxAge         int // seconds, -1 if not present
	MustRevalidate bool
}

// ParseCacheControl parses a Cache-Control header value.
func ParseCacheControl(header string) Directives {
	d := Directives{SMaxAge: -1, MaxAge: -1}
	if header == "" {
		return d
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)

		switch {
		case lower == "no-store":
			d.NoStore = true
		case lower == "no-cache":
			d.NoCache = true
		case lower == "private":
			d.Private = true
		case lower == "public":
			d.Public = true
		case lower == "must-revalidate":
			d.MustRevalidate = true
		case strings.HasPrefix(lower, "s-maxage="):
			if v, err := strconv.Atoi(strings.TrimPrefix(lower, "s-maxage=")); err == nil {
				d.SMaxAge = v
			}
		case strings.HasPrefix(lower, "max-age="):
			if v, err := strconv.Atoi(strings.TrimPrefix(lower, "max-age=")); err == nil {
				d.MaxAge = v
			}
		}
	}
	return d
}

// ShouldCache determines whether a response should be cached.
func ShouldCache(method string, statusCode int, respHeader http.Header) bool {
	// Only cache GET and HEAD
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}

	// Only cache specific status codes
	switch statusCode {
	case http.StatusOK, http.StatusPartialContent,
		http.StatusMovedPermanently, http.StatusFound:
	default:
		return false
	}

	cc := ParseCacheControl(respHeader.Get("Cache-Control"))
	if cc.NoStore || cc.NoCache || cc.Private {
		return false
	}

	return true
}

// ComputeTTL determines the cache duration based on response headers and domain config.
// Priority: force_ttl > s-maxage > max-age > Expires > default_ttl.
func ComputeTTL(respHeader http.Header, domainCfg config.DomainCacheConfig) time.Duration {
	// 1. Forced TTL from CDN config
	if domainCfg.ForceTTL != "" {
		if d, err := config.ParseDuration(domainCfg.ForceTTL); err == nil && d > 0 {
			return d
		}
	}

	cc := ParseCacheControl(respHeader.Get("Cache-Control"))

	// 2. s-maxage
	if cc.SMaxAge >= 0 {
		return time.Duration(cc.SMaxAge) * time.Second
	}

	// 3. max-age
	if cc.MaxAge >= 0 {
		return time.Duration(cc.MaxAge) * time.Second
	}

	// 4. Expires header
	if expires := respHeader.Get("Expires"); expires != "" {
		if t, err := http.ParseTime(expires); err == nil {
			ttl := time.Until(t)
			if ttl > 0 {
				return ttl
			}
		}
	}

	// 5. Default TTL
	if d, err := config.ParseDuration(domainCfg.DefaultTTL); err == nil && d > 0 {
		return d
	}

	return 10 * time.Minute
}
