package cache

import (
	"testing"
)

func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		scheme, host, uri, query string
		ignoreQuery              bool
		expected                 string
	}{
		{"http", "cdn.example.com", "/img/logo.png", "", false, "http://cdn.example.com/img/logo.png"},
		{"https", "cdn.example.com", "/img/logo.png", "v=2&a=1", false, "https://cdn.example.com/img/logo.png?a=1&v=2"},
		{"http", "cdn.example.com", "/img/logo.png", "b=2&a=1", true, "http://cdn.example.com/img/logo.png"},
	}

	for _, tt := range tests {
		got := GenerateCacheKey(tt.scheme, tt.host, tt.uri, tt.query, tt.ignoreQuery)
		if got != tt.expected {
			t.Errorf("GenerateCacheKey(%q,%q,%q,%q,%v) = %q, want %q",
				tt.scheme, tt.host, tt.uri, tt.query, tt.ignoreQuery, got, tt.expected)
		}
	}
}

func TestHashKey(t *testing.T) {
	h1 := HashKey("http://cdn.example.com/test")
	h2 := HashKey("http://cdn.example.com/test")
	h3 := HashKey("http://cdn.example.com/other")

	if h1 != h2 {
		t.Error("same key should produce same hash")
	}
	if h1 == h3 {
		t.Error("different keys should produce different hashes")
	}
	if len(h1) != 64 { // SHA-256 hex
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}
