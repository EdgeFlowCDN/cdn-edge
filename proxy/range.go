package proxy

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ServeRange serves the given body with support for HTTP Range requests.
// If the request contains a valid Range header, it returns 206 Partial Content
// with the appropriate Content-Range header. Otherwise it serves the full body.
func ServeRange(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	totalSize := len(body)

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		// No range request; serve full content.
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(totalSize))
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
	}

	start, end, ok := parseRangeHeader(rangeHeader, totalSize)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
		http.Error(w, "Range Not Satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Serve the partial content.
	partLen := end - start + 1
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
	w.Header().Set("Content-Length", strconv.Itoa(partLen))
	w.WriteHeader(http.StatusPartialContent)
	w.Write(body[start : end+1])
}

// parseRangeHeader parses a Range header value for a single byte range.
// It returns the inclusive start and end byte positions and whether the range is valid.
// Supports formats: "bytes=0-999", "bytes=500-", "bytes=-500".
func parseRangeHeader(rangeHeader string, totalSize int) (start, end int, ok bool) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(rangeHeader, "bytes=")

	// Only support a single range (no multi-range).
	if strings.Contains(spec, ",") {
		return 0, 0, false
	}

	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	if startStr == "" && endStr == "" {
		return 0, 0, false
	}

	if startStr == "" {
		// Suffix range: bytes=-N means last N bytes.
		suffix, err := strconv.Atoi(endStr)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		if suffix > totalSize {
			suffix = totalSize
		}
		return totalSize - suffix, totalSize - 1, true
	}

	start, err := strconv.Atoi(startStr)
	if err != nil || start < 0 || start >= totalSize {
		return 0, 0, false
	}

	if endStr == "" {
		// Open-ended range: bytes=N- means from N to end.
		return start, totalSize - 1, true
	}

	end, err = strconv.Atoi(endStr)
	if err != nil || end < start {
		return 0, 0, false
	}

	// Clamp end to last byte.
	if end >= totalSize {
		end = totalSize - 1
	}

	return start, end, true
}
