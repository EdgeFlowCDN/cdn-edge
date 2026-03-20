package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWAFSQLInjection(t *testing.T) {
	waf := NewWAF()
	tests := []struct {
		uri     string
		blocked bool
	}{
		{"/page?id=1", false},
		{"/page?id=1%27+OR+1%3D1--", true},
		{"/page?q=union+select+*+from+users", true},
		{"/page?q=normal+search", false},
		{"/page?id=1;drop+table+users", true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.uri, nil)
		matched := waf.Check(req)
		if (matched != "") != tt.blocked {
			t.Errorf("WAF(%s): blocked=%v, want %v (rule=%s)", tt.uri, matched != "", tt.blocked, matched)
		}
	}
}

func TestWAFXSS(t *testing.T) {
	waf := NewWAF()
	tests := []struct {
		uri     string
		blocked bool
	}{
		{"/page?q=hello", false},
		{"/page?q=%3Cscript%3Ealert(1)%3C/script%3E", true},
		{"/page?q=javascript:alert(1)", true},
		{"/page?q=onerror%3Dalert(1)", true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.uri, nil)
		matched := waf.Check(req)
		if (matched != "") != tt.blocked {
			t.Errorf("WAF XSS(%s): blocked=%v, want %v", tt.uri, matched != "", tt.blocked)
		}
	}
}

func TestWAFPathTraversal(t *testing.T) {
	waf := NewWAF()
	tests := []struct {
		uri     string
		blocked bool
	}{
		{"/images/logo.png", false},
		{"/../../etc/passwd", true},
		{"/..%2f..%2fetc/passwd", true},
		{"/files/etc/passwd", true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.uri, nil)
		matched := waf.Check(req)
		if (matched != "") != tt.blocked {
			t.Errorf("WAF Path(%s): blocked=%v, want %v", tt.uri, matched != "", tt.blocked)
		}
	}
}

func TestWAFMiddleware(t *testing.T) {
	waf := NewWAF()
	handler := WAFMiddleware(waf, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/page?id=1%27+OR+1%3D1--", nil)
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Errorf("expected 403, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/page?id=1", nil)
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
