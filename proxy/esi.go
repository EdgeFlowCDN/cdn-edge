package proxy

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

var esiIncludeRegex = regexp.MustCompile(`<esi:include\s+src="([^"]+)"\s*/>`)

// ProcessESI scans the response body for ESI include tags and replaces them
// with the content fetched from the referenced URLs. Subrequests go through
// the proxy server itself, so they are cache-aware.
//
// If the content type is not HTML, the body is returned unchanged.
func (s *Server) ProcessESI(body []byte, r *http.Request) []byte {
	matches := esiIncludeRegex.FindAllSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return body
	}

	var result strings.Builder
	result.Grow(len(body))

	prev := 0
	for _, loc := range matches {
		// loc[0]:loc[1] is the full match, loc[2]:loc[3] is the capture group (src URL)
		result.Write(body[prev:loc[0]])

		srcURL := string(body[loc[2]:loc[3]])
		fragment := s.fetchESIFragment(srcURL, r)
		result.WriteString(fragment)

		prev = loc[1]
	}
	result.Write(body[prev:])

	return []byte(result.String())
}

// IsHTMLContentType checks whether the Content-Type indicates HTML.
func IsHTMLContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml")
}

// fetchESIFragment makes a subrequest to the given URL through the proxy.
func (s *Server) fetchESIFragment(srcURL string, originalReq *http.Request) string {
	// Build the subrequest URL
	targetURL := srcURL
	if strings.HasPrefix(srcURL, "/") {
		// Relative URL — build full URL from the original request
		scheme := "http"
		if originalReq.TLS != nil {
			scheme = "https"
		}
		targetURL = scheme + "://" + originalReq.Host + srcURL
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(originalReq.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		cdnlog.Error("esi: failed to create subrequest", "url", srcURL, "error", err)
		return ""
	}

	// Forward relevant headers from the original request
	req.Header.Set("User-Agent", originalReq.Header.Get("User-Agent"))
	req.Header.Set("Accept", originalReq.Header.Get("Accept"))
	if cookie := originalReq.Header.Get("Cookie"); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		cdnlog.Error("esi: subrequest failed", "url", srcURL, "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cdnlog.Warn("esi: subrequest returned non-200", "url", srcURL, "status", resp.StatusCode)
		return ""
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		cdnlog.Error("esi: failed to read subrequest body", "url", srcURL, "error", err)
		return ""
	}

	return string(data)
}
