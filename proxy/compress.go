package proxy

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

// DefaultCompressionMinSize is the default minimum response size to trigger compression.
const DefaultCompressionMinSize = 1024

// compressedContentTypes lists MIME type prefixes that should NOT be compressed
// because they are already compressed formats.
var skipCompressionTypes = []string{
	"image/",
	"video/",
	"audio/",
	"application/zip",
	"application/gzip",
	"application/x-gzip",
	"application/x-bzip2",
	"application/x-xz",
	"application/x-compress",
	"application/x-7z-compressed",
	"application/x-rar-compressed",
	"application/zstd",
	"application/br",
}

var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// CompressionConfig holds settings for the compression middleware.
type CompressionConfig struct {
	MinSize int // minimum response body size in bytes to enable compression
}

// CompressionMiddleware returns an HTTP middleware that compresses responses
// using brotli or gzip based on the client's Accept-Encoding header.
func CompressionMiddleware(cfg CompressionConfig, next http.Handler) http.Handler {
	minSize := cfg.MinSize
	if minSize <= 0 {
		minSize = DefaultCompressionMinSize
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Determine the best encoding the client accepts.
		encoding := selectEncoding(r.Header.Get("Accept-Encoding"))
		if encoding == "" {
			next.ServeHTTP(w, r)
			return
		}

		cw := &compressResponseWriter{
			ResponseWriter: w,
			encoding:       encoding,
			minSize:        minSize,
		}
		defer cw.Close()

		next.ServeHTTP(cw, r)
	})
}

// selectEncoding picks the best compression encoding from the Accept-Encoding header.
// It prefers brotli over gzip.
func selectEncoding(acceptEncoding string) string {
	hasBr := false
	hasGzip := false

	for _, part := range strings.Split(acceptEncoding, ",") {
		enc := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		switch enc {
		case "br":
			hasBr = true
		case "gzip":
			hasGzip = true
		}
	}

	if hasBr {
		return "br"
	}
	if hasGzip {
		return "gzip"
	}
	return ""
}

// compressResponseWriter intercepts writes to conditionally compress the response.
// It buffers initial bytes to decide whether to compress based on content type and size.
type compressResponseWriter struct {
	http.ResponseWriter
	encoding    string
	minSize     int
	writer      io.WriteCloser // the compression writer, nil until decided
	buf         []byte         // buffer for initial bytes before decision
	decided     bool           // whether we have decided to compress or not
	compressing bool           // true if we decided to compress
	statusCode  int            // buffered status code
	wroteHeader bool
}

func (cw *compressResponseWriter) WriteHeader(code int) {
	if cw.wroteHeader {
		return
	}
	cw.statusCode = code
	cw.wroteHeader = true

	// If Content-Encoding is already set, skip compression entirely.
	if cw.ResponseWriter.Header().Get("Content-Encoding") != "" {
		cw.decided = true
		cw.compressing = false
		cw.ResponseWriter.WriteHeader(code)
	}
}

func (cw *compressResponseWriter) Write(p []byte) (int, error) {
	if !cw.wroteHeader {
		cw.WriteHeader(http.StatusOK)
	}

	// If already decided, write directly.
	if cw.decided {
		if cw.compressing {
			return cw.writer.Write(p)
		}
		return cw.ResponseWriter.Write(p)
	}

	// Buffer data until we have enough to decide.
	cw.buf = append(cw.buf, p...)
	if len(cw.buf) < cw.minSize {
		// Not enough data yet; wait for more writes or Close.
		return len(p), nil
	}

	// We have enough data; make the decision now.
	cw.flush()
	return len(p), nil
}

// flush makes the compress/no-compress decision and writes buffered data.
func (cw *compressResponseWriter) flush() {
	if cw.decided {
		return
	}
	cw.decided = true

	ct := cw.ResponseWriter.Header().Get("Content-Type")
	if shouldSkipCompression(ct) || len(cw.buf) < cw.minSize {
		cw.compressing = false
		cw.ResponseWriter.WriteHeader(cw.statusCode)
		if len(cw.buf) > 0 {
			cw.ResponseWriter.Write(cw.buf)
		}
		cw.buf = nil
		return
	}

	// Enable compression.
	cw.compressing = true
	cw.ResponseWriter.Header().Set("Content-Encoding", cw.encoding)
	cw.ResponseWriter.Header().Add("Vary", "Accept-Encoding")
	cw.ResponseWriter.Header().Del("Content-Length") // length will change
	cw.ResponseWriter.WriteHeader(cw.statusCode)

	cw.writer = newCompressWriter(cw.ResponseWriter, cw.encoding)
	if len(cw.buf) > 0 {
		cw.writer.Write(cw.buf)
	}
	cw.buf = nil
}

// Close finalizes the compression writer and flushes any remaining buffered data.
func (cw *compressResponseWriter) Close() {
	if !cw.decided {
		cw.flush()
	}
	if cw.writer != nil {
		cw.writer.Close()
		if cw.encoding == "gzip" {
			if gw, ok := cw.writer.(*gzip.Writer); ok {
				gw.Reset(io.Discard)
				gzipWriterPool.Put(gw)
			}
		}
	}
}

// Hijack implements http.Hijacker for websocket support.
func (cw *compressResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := cw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Flush implements http.Flusher.
func (cw *compressResponseWriter) Flush() {
	if !cw.decided {
		cw.flush()
	}
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func newCompressWriter(w io.Writer, encoding string) io.WriteCloser {
	switch encoding {
	case "br":
		return brotli.NewWriter(w)
	case "gzip":
		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		return gw
	default:
		return nil
	}
}

// shouldSkipCompression returns true if the content type should not be compressed.
func shouldSkipCompression(contentType string) bool {
	if contentType == "" {
		return false
	}
	ct := strings.ToLower(strings.SplitN(contentType, ";", 2)[0])
	ct = strings.TrimSpace(ct)
	for _, skip := range skipCompressionTypes {
		if strings.HasPrefix(ct, skip) {
			return true
		}
	}
	return false
}
