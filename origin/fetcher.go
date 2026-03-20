package origin

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/EdgeFlowCDN/cdn-edge/config"
	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// FetchResult is the result of an origin fetch.
type FetchResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Fetcher handles origin requests with coalescing, retries, and timeouts.
type Fetcher struct {
	client   *http.Client
	group    singleflight.Group
	strategy Strategy
	maxRetry int
}

// NewFetcher creates a new origin fetcher.
func NewFetcher(strategyName string) *Fetcher {
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).DialContext,
	}
	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		strategy: NewStrategy(strategyName),
		maxRetry: 2,
	}
}

// Fetch retrieves a resource from the origin. Concurrent requests for the same
// cache key are coalesced into a single origin request.
func (f *Fetcher) Fetch(ctx context.Context, cacheKey string, req *http.Request, origins []config.OriginConfig) (*FetchResult, error) {
	result, err, _ := f.group.Do(cacheKey, func() (interface{}, error) {
		return f.doFetch(ctx, req, origins)
	})
	if err != nil {
		return nil, err
	}
	return result.(*FetchResult), nil
}

func (f *Fetcher) doFetch(ctx context.Context, req *http.Request, origins []config.OriginConfig) (*FetchResult, error) {
	var lastErr error

	for attempt := 0; attempt <= f.maxRetry; attempt++ {
		origin := f.strategy.Select(origins, attempt)
		if origin == nil {
			return nil, fmt.Errorf("no available origin")
		}

		result, err := f.fetchFromOrigin(ctx, req, origin)
		if err != nil {
			cdnlog.Warn("origin fetch failed",
				"origin", origin.Addr,
				"attempt", attempt,
				"error", err,
			)
			lastErr = err
			continue
		}

		// Retry on 5xx
		if result.StatusCode >= 500 {
			cdnlog.Warn("origin returned 5xx",
				"origin", origin.Addr,
				"status", result.StatusCode,
				"attempt", attempt,
			)
			lastErr = fmt.Errorf("origin returned %d", result.StatusCode)
			continue
		}

		return result, nil
	}

	return nil, fmt.Errorf("all origin attempts failed: %w", lastErr)
}

func (f *Fetcher) fetchFromOrigin(ctx context.Context, originalReq *http.Request, origin *config.OriginConfig) (*FetchResult, error) {
	// Build origin URL
	originURL := strings.TrimRight(origin.Addr, "/") + originalReq.URL.RequestURI()

	req, err := http.NewRequestWithContext(ctx, originalReq.Method, originURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create origin request: %w", err)
	}

	// Forward headers
	for key, values := range originalReq.Header {
		lk := strings.ToLower(key)
		// Skip hop-by-hop headers
		if isHopByHop(lk) {
			continue
		}
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}

	// Set forwarding headers
	clientIP := clientIPFromRequest(originalReq)
	if existing := req.Header.Get("X-Forwarded-For"); existing != "" {
		req.Header.Set("X-Forwarded-For", existing+", "+clientIP)
	} else {
		req.Header.Set("X-Forwarded-For", clientIP)
	}
	req.Header.Set("X-Real-IP", clientIP)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("origin request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read origin response: %w", err)
	}

	return &FetchResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
	}, nil
}

var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

func isHopByHop(header string) bool {
	return hopByHopHeaders[header]
}

func clientIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
