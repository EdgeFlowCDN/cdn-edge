package proxy

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestServeRange_FullContent(t *testing.T) {
	body := []byte("Hello, World!")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("expected Accept-Ranges: bytes")
	}
	if w.Body.String() != "Hello, World!" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestServeRange_SimpleRange(t *testing.T) {
	body := []byte("Hello, World!")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	r.Header.Set("Range", "bytes=0-4")
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if w.Body.String() != "Hello" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
	cr := resp.Header.Get("Content-Range")
	expected := "bytes 0-4/13"
	if cr != expected {
		t.Fatalf("expected Content-Range %q, got %q", expected, cr)
	}
	cl := resp.Header.Get("Content-Length")
	if cl != "5" {
		t.Fatalf("expected Content-Length 5, got %s", cl)
	}
}

func TestServeRange_OpenEnded(t *testing.T) {
	body := []byte("Hello, World!")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	r.Header.Set("Range", "bytes=7-")
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if w.Body.String() != "World!" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestServeRange_SuffixRange(t *testing.T) {
	body := []byte("Hello, World!")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	r.Header.Set("Range", "bytes=-6")
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if w.Body.String() != "orld!!" {
		// "World!" is the last 6 chars
		got := w.Body.String()
		if got != "World!" {
			t.Fatalf("unexpected body: %q", got)
		}
	}
}

func TestServeRange_InvalidRange(t *testing.T) {
	body := []byte("Hello")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	r.Header.Set("Range", "bytes=10-20")
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("expected 416, got %d", resp.StatusCode)
	}
}

func TestServeRange_InvalidFormat(t *testing.T) {
	body := []byte("Hello")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	r.Header.Set("Range", "invalid")
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("expected 416, got %d", resp.StatusCode)
	}
}

func TestServeRange_ClampEnd(t *testing.T) {
	body := []byte("Hello")
	r := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	r.Header.Set("Range", "bytes=0-999")
	w := httptest.NewRecorder()

	ServeRange(w, r, body, "text/plain")

	resp := w.Result()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if w.Body.String() != "Hello" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
	cl, _ := strconv.Atoi(resp.Header.Get("Content-Length"))
	if cl != 5 {
		t.Fatalf("expected Content-Length 5, got %d", cl)
	}
}

func TestParseRangeHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		totalSize int
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{"simple range", "bytes=0-9", 100, 0, 9, true},
		{"open ended", "bytes=50-", 100, 50, 99, true},
		{"suffix", "bytes=-10", 100, 90, 99, true},
		{"suffix larger than file", "bytes=-200", 100, 0, 99, true},
		{"no prefix", "chars=0-9", 100, 0, 0, false},
		{"multi range", "bytes=0-9,20-29", 100, 0, 0, false},
		{"empty", "bytes=-", 100, 0, 0, false},
		{"start beyond size", "bytes=100-200", 100, 0, 0, false},
		{"end before start", "bytes=50-10", 100, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, ok := parseRangeHeader(tt.header, tt.totalSize)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if start != tt.wantStart || end != tt.wantEnd {
					t.Fatalf("range = %d-%d, want %d-%d", start, end, tt.wantStart, tt.wantEnd)
				}
			}
		})
	}
}
