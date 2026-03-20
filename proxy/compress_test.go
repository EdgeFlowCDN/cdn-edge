package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func makeHandler(body string, contentType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Write([]byte(body))
	})
}

func TestCompressionMiddleware_GzipCompresses(t *testing.T) {
	// Body must be >= minSize to trigger compression.
	body := strings.Repeat("a", 2048)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "text/plain"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", resp.Header.Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()
	decoded, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to read gzip: %v", err)
	}
	if string(decoded) != body {
		t.Fatalf("decoded body mismatch: got %d bytes, want %d", len(decoded), len(body))
	}
}

func TestCompressionMiddleware_BrotliCompresses(t *testing.T) {
	body := strings.Repeat("b", 2048)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "text/html"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "br, gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "br" {
		t.Fatalf("expected Content-Encoding: br, got %q", resp.Header.Get("Content-Encoding"))
	}

	decoded, err := io.ReadAll(brotli.NewReader(w.Body))
	if err != nil {
		t.Fatalf("failed to read brotli: %v", err)
	}
	if string(decoded) != body {
		t.Fatalf("decoded body mismatch")
	}
}

func TestCompressionMiddleware_SmallResponseNotCompressed(t *testing.T) {
	body := "small"
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "text/plain"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for small response, got %q", resp.Header.Get("Content-Encoding"))
	}
	if w.Body.String() != body {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestCompressionMiddleware_ImageNotCompressed(t *testing.T) {
	body := strings.Repeat("x", 2048)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "image/png"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for image, got %q", resp.Header.Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_AlreadyEncodedNotCompressed(t *testing.T) {
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.Repeat("z", 2048)))
	})

	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		innerHandler,
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	// Should keep the original Content-Encoding, not double-compress.
	if resp.Header.Get("Content-Encoding") != "br" {
		t.Fatalf("expected Content-Encoding: br (original), got %q", resp.Header.Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_NoAcceptEncoding(t *testing.T) {
	body := strings.Repeat("y", 2048)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "text/plain"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Accept-Encoding header.
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("expected no compression without Accept-Encoding")
	}
	if w.Body.String() != body {
		t.Fatalf("unexpected body")
	}
}

func TestCompressionMiddleware_VaryHeader(t *testing.T) {
	body := strings.Repeat("v", 2048)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "text/plain"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	vary := resp.Header.Get("Vary")
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("expected Vary to contain Accept-Encoding, got %q", vary)
	}
}

func TestShouldSkipCompression(t *testing.T) {
	tests := []struct {
		ct   string
		skip bool
	}{
		{"text/plain", false},
		{"text/html; charset=utf-8", false},
		{"application/json", false},
		{"image/png", true},
		{"image/jpeg", true},
		{"video/mp4", true},
		{"audio/mpeg", true},
		{"application/zip", true},
		{"application/gzip", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			if got := shouldSkipCompression(tt.ct); got != tt.skip {
				t.Fatalf("shouldSkipCompression(%q) = %v, want %v", tt.ct, got, tt.skip)
			}
		})
	}
}

func TestSelectEncoding(t *testing.T) {
	tests := []struct {
		accept string
		want   string
	}{
		{"gzip", "gzip"},
		{"br", "br"},
		{"gzip, br", "br"},
		{"br;q=1.0, gzip;q=0.8", "br"},
		{"deflate", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			if got := selectEncoding(tt.accept); got != tt.want {
				t.Fatalf("selectEncoding(%q) = %q, want %q", tt.accept, got, tt.want)
			}
		})
	}
}

func TestCompressionMiddleware_VideoNotCompressed(t *testing.T) {
	body := strings.Repeat("m", 2048)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		makeHandler(body, "video/mp4"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip, br")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Result().Header.Get("Content-Encoding") != "" {
		t.Fatalf("expected no compression for video")
	}
}

func TestCompressionMiddleware_DefaultMinSize(t *testing.T) {
	// With zero MinSize, it should default to DefaultCompressionMinSize (1024).
	body := strings.Repeat("d", 500)
	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 0},
		makeHandler(body, "text/plain"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Result().Header.Get("Content-Encoding") != "" {
		t.Fatalf("expected no compression for response under default min size")
	}
}

// Verify gzip output is valid when written in multiple chunks.
func TestCompressionMiddleware_MultipleWrites(t *testing.T) {
	chunk := strings.Repeat("c", 600)
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Write in multiple chunks; total > minSize.
		w.Write([]byte(chunk))
		w.Write([]byte(chunk))
	})

	handler := CompressionMiddleware(
		CompressionConfig{MinSize: 1024},
		innerHandler,
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", resp.Header.Get("Content-Encoding"))
	}

	gr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	decoded, _ := io.ReadAll(gr)
	if string(decoded) != chunk+chunk {
		t.Fatalf("decoded body mismatch: got %d bytes, want %d", len(decoded), len(chunk)*2)
	}
}
