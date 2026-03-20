package proxy

import (
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/EdgeFlowCDN/cdn-edge/cache"
	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
	"github.com/EdgeFlowCDN/cdn-edge/origin"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := uuid.New().String()

	// Match domain
	domainCfg, ok := s.domains[r.Host]
	if !ok {
		// Try without port
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			domainCfg, ok = s.domains[h]
		}
	}
	if !ok {
		http.Error(w, "Forbidden", http.StatusForbidden)
		s.logAccess(r, requestID, http.StatusForbidden, 0, string(cache.StatusMiss), 0, 0, start)
		return
	}

	// Determine scheme
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	// Generate cache key
	cacheKey := cache.GenerateCacheKey(scheme, r.Host, r.URL.Path, r.URL.RawQuery, domainCfg.Cache.IgnoreQuery)

	// Check cache
	entry, cacheStatus := s.cacheManager.Get(cacheKey)
	if entry != nil {
		s.writeResponse(w, entry, cacheStatus, requestID)
		s.logAccess(r, requestID, entry.StatusCode, int64(len(entry.Body)), string(cacheStatus), 0, 0, start)
		return
	}

	// Fetch from origin
	originStart := time.Now()
	result, err := s.fetcher.Fetch(r.Context(), cacheKey, r, domainCfg.Origins)
	originDuration := time.Since(originStart).Seconds()

	if err != nil {
		cdnlog.Error("origin fetch failed", "key", cacheKey, "error", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		s.logAccess(r, requestID, http.StatusBadGateway, 0, string(cache.StatusMiss), 0, originDuration, start)
		return
	}

	// Decide whether to cache
	if cache.ShouldCache(r.Method, result.StatusCode, result.Header) {
		ttl := cache.ComputeTTL(result.Header, domainCfg.Cache)
		entry := &cache.Entry{
			StatusCode: result.StatusCode,
			Header:     result.Header.Clone(),
			Body:       result.Body,
			Size:       int64(len(result.Body)),
			CreatedAt:  time.Now(),
			ExpiresAt:  time.Now().Add(ttl),
			Key:        cacheKey,
			Hash:       cache.HashKey(cacheKey),
		}
		s.cacheManager.Put(cacheKey, entry)
	}

	// Write response
	s.writeOriginResponse(w, result, requestID)
	s.logAccess(r, requestID, result.StatusCode, int64(len(result.Body)), string(cache.StatusMiss), result.StatusCode, originDuration, start)
}

func (s *Server) writeResponse(w http.ResponseWriter, entry *cache.Entry, status cache.CacheStatus, requestID string) {
	for key, values := range entry.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("X-Cache", string(status))
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(entry.StatusCode)
	w.Write(entry.Body)
}

func (s *Server) writeOriginResponse(w http.ResponseWriter, result *origin.FetchResult, requestID string) {
	for key, values := range result.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("X-Cache", string(cache.StatusMiss))
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(result.StatusCode)
	w.Write(result.Body)
}

func (s *Server) logAccess(r *http.Request, requestID string, status int, bodyBytes int64, cacheStatus string, upstreamStatus int, upstreamTime float64, start time.Time) {
	if s.accessLogger == nil {
		return
	}

	clientIP := r.Header.Get("X-Real-IP")
	if clientIP == "" {
		clientIP, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	s.accessLogger.Log(cdnlog.AccessEntry{
		Time:           start,
		ClientIP:       clientIP,
		Method:         r.Method,
		Host:           r.Host,
		URI:            r.RequestURI,
		Status:         status,
		BodyBytes:      bodyBytes,
		CacheStatus:    cacheStatus,
		UpstreamStatus: upstreamStatus,
		UpstreamTime:   upstreamTime,
		RequestTime:    time.Since(start).Seconds(),
		Referer:        r.Header.Get("Referer"),
		UserAgent:      r.Header.Get("User-Agent"),
		RequestID:      requestID,
	})
}
