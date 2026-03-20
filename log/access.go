package log

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// AccessEntry represents a single access log record.
type AccessEntry struct {
	Time           time.Time `json:"time"`
	ClientIP       string    `json:"client_ip"`
	Method         string    `json:"method"`
	Host           string    `json:"host"`
	URI            string    `json:"uri"`
	Status         int       `json:"status"`
	BodyBytes      int64     `json:"body_bytes"`
	CacheStatus    string    `json:"cache_status"`
	UpstreamStatus int       `json:"upstream_status"`
	UpstreamTime   float64   `json:"upstream_time"`
	RequestTime    float64   `json:"request_time"`
	Referer        string    `json:"referer"`
	UserAgent      string    `json:"user_agent"`
	RequestID      string    `json:"request_id"`
}

// AccessLogger writes structured JSON access logs to a file.
type AccessLogger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// NewAccessLogger creates a new access logger writing to the given path.
// If path is empty, logs go to stdout.
func NewAccessLogger(path string) (*AccessLogger, error) {
	var f *os.File
	var err error

	if path == "" {
		f = os.Stdout
	} else {
		f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
	}

	return &AccessLogger{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Log writes an access log entry.
func (al *AccessLogger) Log(entry AccessEntry) {
	al.mu.Lock()
	defer al.mu.Unlock()
	_ = al.enc.Encode(entry)
}

// Close closes the underlying file.
func (al *AccessLogger) Close() error {
	if al.file != os.Stdout {
		return al.file.Close()
	}
	return nil
}
