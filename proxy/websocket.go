package proxy

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

const (
	wsIdleTimeout = 60 * time.Second
	wsDialTimeout = 10 * time.Second
)

// IsWebSocketUpgrade checks if the request is a WebSocket upgrade request.
func IsWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		containsToken(r.Header.Get("Connection"), "upgrade")
}

// containsToken checks if a comma-separated header value contains a specific token (case-insensitive).
func containsToken(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// HandleWebSocket hijacks the client connection and proxies WebSocket traffic
// to the given origin address.
func HandleWebSocket(w http.ResponseWriter, r *http.Request, originAddr string) {
	// Hijack the client connection
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket hijack not supported", http.StatusInternalServerError)
		return
	}

	// Dial origin
	originURL := strings.TrimRight(originAddr, "/") + r.URL.RequestURI()
	// Convert http(s) to ws target host
	targetHost := extractHost(originURL)
	if targetHost == "" {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	originConn, err := net.DialTimeout("tcp", targetHost, wsDialTimeout)
	if err != nil {
		cdnlog.Error("websocket dial origin failed", "target", targetHost, "error", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	clientConn, bufrw, err := hj.Hijack()
	if err != nil {
		cdnlog.Error("websocket hijack failed", "error", err)
		originConn.Close()
		return
	}

	// Forward the original HTTP upgrade request to the origin
	if err := r.Write(originConn); err != nil {
		cdnlog.Error("websocket write upgrade request failed", "error", err)
		clientConn.Close()
		originConn.Close()
		return
	}

	// Flush any buffered data from the hijacked connection
	if bufrw.Reader.Buffered() > 0 {
		buffered := make([]byte, bufrw.Reader.Buffered())
		_, _ = bufrw.Read(buffered)
		_, _ = originConn.Write(buffered)
	}

	// Bidirectional copy with idle timeout
	done := make(chan struct{})

	go func() {
		defer func() { done <- struct{}{} }()
		copyWithIdleTimeout(originConn, clientConn)
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		copyWithIdleTimeout(clientConn, originConn)
	}()

	// Wait for one direction to finish, then close both
	<-done
	clientConn.Close()
	originConn.Close()
	<-done

	cdnlog.Debug("websocket connection closed", "path", r.URL.Path)
}

// copyWithIdleTimeout copies from src to dst, resetting a deadline on each successful read.
func copyWithIdleTimeout(dst net.Conn, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		_ = src.SetReadDeadline(time.Now().Add(wsIdleTimeout))
		n, readErr := src.Read(buf)
		if n > 0 {
			_ = dst.SetWriteDeadline(time.Now().Add(wsIdleTimeout))
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				cdnlog.Debug("websocket copy error", "error", readErr)
			}
			return
		}
	}
}

// extractHost extracts the host:port from a URL string for TCP dialing.
// It handles http:// and https:// schemes.
func extractHost(rawURL string) string {
	// Strip scheme
	u := rawURL
	if idx := strings.Index(u, "://"); idx != -1 {
		u = u[idx+3:]
	}
	// Strip path
	if idx := strings.Index(u, "/"); idx != -1 {
		u = u[:idx]
	}
	// Add default port if missing
	if _, _, err := net.SplitHostPort(u); err != nil {
		if strings.HasPrefix(rawURL, "https") {
			return u + ":443"
		}
		return u + ":80"
	}
	return u
}
