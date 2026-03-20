package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EdgeFlowCDN/cdn-edge/cache"
	"github.com/EdgeFlowCDN/cdn-edge/config"
	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
	"github.com/EdgeFlowCDN/cdn-edge/proxy"
)

func init() {
	_ = cdnlog.Init("error", "")
}

func setupTestServer(t *testing.T, originHandler http.HandlerFunc) (*httptest.Server, *httptest.Server) {
	t.Helper()

	origin := httptest.NewServer(originHandler)

	cfg := &config.Config{
		Server: config.ServerConfig{Listen: ":0"},
		Cache: config.CacheConfig{
			Memory: config.MemoryCacheConfig{MaxSize: "10MB", MaxObjectSize: "1MB"},
			Disk:   config.DiskCacheConfig{Path: t.TempDir(), MaxSize: "100MB", MaxObjectSize: "10MB"},
		},
		Domains: []config.DomainConfig{
			{
				Host: "cdn.test.com",
				Origins: []config.OriginConfig{
					{Addr: origin.URL, Weight: 100},
				},
				Cache: config.DomainCacheConfig{DefaultTTL: "10m"},
			},
		},
	}

	memCache := cache.NewMemoryCache(10*1024*1024, 1*1024*1024)
	diskCache, err := cache.NewDiskCache(cfg.Cache.Disk.Path, 100*1024*1024, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { diskCache.Stop() })

	cacheManager := cache.NewManager(memCache, diskCache)
	server := proxy.NewServer(cfg, cacheManager, nil)

	proxyServer := httptest.NewServer(server)
	return origin, proxyServer
}

func TestProxyCacheMissAndHit(t *testing.T) {
	originHits := 0
	origin, proxyServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello from origin"))
	})
	defer origin.Close()
	defer proxyServer.Close()

	client := proxyServer.Client()

	// First request — MISS
	req, _ := http.NewRequest("GET", proxyServer.URL+"/test.txt", nil)
	req.Host = "cdn.test.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "hello from origin" {
		t.Errorf("body = %q, want %q", string(body), "hello from origin")
	}
	if resp.Header.Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", resp.Header.Get("X-Cache"))
	}
	if originHits != 1 {
		t.Errorf("originHits = %d, want 1", originHits)
	}

	// Second request — HIT-MEM
	req, _ = http.NewRequest("GET", proxyServer.URL+"/test.txt", nil)
	req.Host = "cdn.test.com"
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "hello from origin" {
		t.Errorf("body = %q, want %q", string(body), "hello from origin")
	}
	if resp.Header.Get("X-Cache") != "HIT-MEM" {
		t.Errorf("X-Cache = %q, want HIT-MEM", resp.Header.Get("X-Cache"))
	}
	if originHits != 1 {
		t.Errorf("originHits = %d after cache hit, want 1", originHits)
	}
}

func TestProxyUnknownHostReturns403(t *testing.T) {
	origin, proxyServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	defer origin.Close()
	defer proxyServer.Close()

	req, _ := http.NewRequest("GET", proxyServer.URL+"/test.txt", nil)
	req.Host = "unknown.host.com"
	resp, err := proxyServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestProxyNoStoreNotCached(t *testing.T) {
	originHits := 0
	origin, proxyServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("no-store content"))
	})
	defer origin.Close()
	defer proxyServer.Close()

	client := proxyServer.Client()

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", proxyServer.URL+"/private.txt", nil)
		req.Host = "cdn.test.com"
		resp, _ := client.Do(req)
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	if originHits != 3 {
		t.Errorf("originHits = %d, want 3 (no-store should not cache)", originHits)
	}
}

func TestProxyPOSTNotCached(t *testing.T) {
	originHits := 0
	origin, proxyServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write([]byte("post response"))
	})
	defer origin.Close()
	defer proxyServer.Close()

	client := proxyServer.Client()

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", proxyServer.URL+"/api", nil)
		req.Host = "cdn.test.com"
		resp, _ := client.Do(req)
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	if originHits != 2 {
		t.Errorf("originHits = %d, want 2 (POST should not cache)", originHits)
	}
}

func TestProxyRequestID(t *testing.T) {
	origin, proxyServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("ok"))
	})
	defer origin.Close()
	defer proxyServer.Close()

	req, _ := http.NewRequest("GET", proxyServer.URL+"/test", nil)
	req.Host = "cdn.test.com"
	resp, _ := proxyServer.Client().Do(req)
	resp.Body.Close()

	rid := resp.Header.Get("X-Request-ID")
	if rid == "" {
		t.Error("X-Request-ID should be set")
	}

	// Second request should have a different ID
	req, _ = http.NewRequest("GET", proxyServer.URL+"/test", nil)
	req.Host = "cdn.test.com"
	resp2, _ := proxyServer.Client().Do(req)
	resp2.Body.Close()

	rid2 := resp2.Header.Get("X-Request-ID")
	if rid == rid2 {
		t.Error("each request should have a unique X-Request-ID")
	}
}

func BenchmarkProxyCacheHit(b *testing.B) {
	_ = cdnlog.Init("error", "")
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write([]byte("benchmark content"))
	}))
	defer origin.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{Listen: ":0"},
		Domains: []config.DomainConfig{
			{
				Host:    "bench.test.com",
				Origins: []config.OriginConfig{{Addr: origin.URL, Weight: 100}},
				Cache:   config.DomainCacheConfig{DefaultTTL: "10m"},
			},
		},
	}

	memCache := cache.NewMemoryCache(100*1024*1024, 1*1024*1024)
	cacheManager := cache.NewManager(memCache, nil)
	server := proxy.NewServer(cfg, cacheManager, nil)
	proxyServer := httptest.NewServer(server)
	defer proxyServer.Close()

	// Prime the cache
	req, _ := http.NewRequest("GET", proxyServer.URL+"/bench", nil)
	req.Host = "bench.test.com"
	resp, _ := proxyServer.Client().Do(req)
	io.ReadAll(resp.Body)
	resp.Body.Close()

	// Wait a moment for cache write
	time.Sleep(10 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", proxyServer.URL+"/bench", nil)
			req.Host = "bench.test.com"
			resp, _ := proxyServer.Client().Do(req)
			io.ReadAll(resp.Body)
			resp.Body.Close()
		}
	})
}
