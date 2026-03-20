package cache

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Entry represents a cached HTTP response.
type Entry struct {
	StatusCode int         `json:"status_code"`
	Header     http.Header `json:"header"`
	Body       []byte      `json:"-"`
	Size       int64       `json:"size"`
	CreatedAt  time.Time   `json:"created_at"`
	ExpiresAt  time.Time   `json:"expires_at"`
	Key        string      `json:"key"`
	Hash       string      `json:"hash"`
}

// IsExpired returns true if the entry has expired.
func (e *Entry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// GenerateCacheKey creates a canonical cache key from request components.
func GenerateCacheKey(scheme, host, uri, rawQuery string, ignoreQuery bool) string {
	key := scheme + "://" + host + uri
	if !ignoreQuery && rawQuery != "" {
		key += "?" + sortQuery(rawQuery)
	}
	return key
}

// HashKey returns the SHA-256 hex digest of a cache key.
func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h)
}

func sortQuery(rawQuery string) string {
	pairs := strings.Split(rawQuery, "&")
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}
