package cache

import (
	"net/http"
	"testing"
	"time"

	"github.com/EdgeFlowCDN/cdn-edge/config"
)

func TestParseCacheControl(t *testing.T) {
	tests := []struct {
		header string
		check  func(Directives) bool
		desc   string
	}{
		{"no-store", func(d Directives) bool { return d.NoStore }, "no-store"},
		{"no-cache", func(d Directives) bool { return d.NoCache }, "no-cache"},
		{"private", func(d Directives) bool { return d.Private }, "private"},
		{"public", func(d Directives) bool { return d.Public }, "public"},
		{"max-age=3600", func(d Directives) bool { return d.MaxAge == 3600 }, "max-age=3600"},
		{"s-maxage=600", func(d Directives) bool { return d.SMaxAge == 600 }, "s-maxage=600"},
		{"public, max-age=86400, s-maxage=3600", func(d Directives) bool {
			return d.Public && d.MaxAge == 86400 && d.SMaxAge == 3600
		}, "combined"},
		{"", func(d Directives) bool { return d.MaxAge == -1 && d.SMaxAge == -1 }, "empty"},
	}

	for _, tt := range tests {
		d := ParseCacheControl(tt.header)
		if !tt.check(d) {
			t.Errorf("ParseCacheControl(%q): %s check failed", tt.header, tt.desc)
		}
	}
}

func TestShouldCache(t *testing.T) {
	tests := []struct {
		method     string
		statusCode int
		cc         string
		want       bool
	}{
		{"GET", 200, "public, max-age=3600", true},
		{"GET", 200, "no-store", false},
		{"GET", 200, "no-cache", false},
		{"GET", 200, "private", false},
		{"POST", 200, "public", false},
		{"GET", 404, "public", false},
		{"GET", 301, "public", true},
		{"HEAD", 200, "public", true},
		{"GET", 206, "public", true},
	}

	for _, tt := range tests {
		h := http.Header{}
		if tt.cc != "" {
			h.Set("Cache-Control", tt.cc)
		}
		got := ShouldCache(tt.method, tt.statusCode, h)
		if got != tt.want {
			t.Errorf("ShouldCache(%s, %d, %q) = %v, want %v",
				tt.method, tt.statusCode, tt.cc, got, tt.want)
		}
	}
}

func TestComputeTTL(t *testing.T) {
	domainCfg := config.DomainCacheConfig{DefaultTTL: "10m"}

	// s-maxage takes priority over max-age
	h := http.Header{}
	h.Set("Cache-Control", "max-age=3600, s-maxage=600")
	ttl := ComputeTTL(h, domainCfg)
	if ttl != 600*time.Second {
		t.Errorf("expected 600s, got %v", ttl)
	}

	// max-age without s-maxage
	h = http.Header{}
	h.Set("Cache-Control", "max-age=3600")
	ttl = ComputeTTL(h, domainCfg)
	if ttl != 3600*time.Second {
		t.Errorf("expected 3600s, got %v", ttl)
	}

	// No cache headers -> default TTL
	h = http.Header{}
	ttl = ComputeTTL(h, domainCfg)
	if ttl != 10*time.Minute {
		t.Errorf("expected 10m, got %v", ttl)
	}

	// Force TTL overrides everything
	forceCfg := config.DomainCacheConfig{ForceTTL: "1h", DefaultTTL: "10m"}
	h = http.Header{}
	h.Set("Cache-Control", "max-age=60")
	ttl = ComputeTTL(h, forceCfg)
	if ttl != 1*time.Hour {
		t.Errorf("expected 1h, got %v", ttl)
	}
}
