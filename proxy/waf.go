package proxy

import (
	"net/http"
	"regexp"
	"strings"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// WAFRule defines a single WAF detection rule.
type WAFRule struct {
	Name    string
	Pattern *regexp.Regexp
	Targets []string // "uri", "query", "body", "headers"
}

// WAF performs basic web application firewall checks.
type WAF struct {
	rules   []WAFRule
	enabled bool
}

// NewWAF creates a WAF with default rules for SQL injection, XSS, and path traversal.
func NewWAF() *WAF {
	return &WAF{
		enabled: true,
		rules: []WAFRule{
			// SQL Injection patterns
			{
				Name:    "sql-injection-keywords",
				Pattern: regexp.MustCompile(`(?i)(union\s+select|select\s+.*\s+from|insert\s+into|delete\s+from|drop\s+table|update\s+.*\s+set|or\s+1\s*=\s*1|and\s+1\s*=\s*1|'\s*or\s*'|'\s*;\s*drop)`),
				Targets: []string{"uri", "query"},
			},
			{
				Name:    "sql-injection-chars",
				Pattern: regexp.MustCompile(`(?i)(\b(exec|execute|sp_|xp_)\b|--|;.*\b(drop|alter|create)\b)`),
				Targets: []string{"uri", "query"},
			},
			// XSS patterns
			{
				Name:    "xss-script-tag",
				Pattern: regexp.MustCompile(`(?i)(<script[^>]*>|</script>|javascript\s*:|on(load|error|click|mouseover|focus|blur)\s*=)`),
				Targets: []string{"uri", "query"},
			},
			{
				Name:    "xss-event-handler",
				Pattern: regexp.MustCompile(`(?i)(eval\s*\(|alert\s*\(|document\.(cookie|write|location)|window\.location)`),
				Targets: []string{"uri", "query"},
			},
			// Path traversal
			{
				Name:    "path-traversal",
				Pattern: regexp.MustCompile(`(\.\.[\\/]|\.\.%2[fF]|%2[eE]%2[eE][\\/]|\.\.%255[cC])`),
				Targets: []string{"uri"},
			},
			{
				Name:    "path-traversal-encoded",
				Pattern: regexp.MustCompile(`(?i)(/etc/passwd|/etc/shadow|/proc/self|/windows/system32)`),
				Targets: []string{"uri"},
			},
			// Common attack patterns
			{
				Name:    "null-byte",
				Pattern: regexp.MustCompile(`%00`),
				Targets: []string{"uri", "query"},
			},
		},
	}
}

// Check inspects a request and returns the matched rule name if blocked, or empty string if allowed.
func (w *WAF) Check(r *http.Request) string {
	if !w.enabled {
		return ""
	}

	for _, rule := range w.rules {
		for _, target := range rule.Targets {
			var values []string
			switch target {
			case "uri":
				values = []string{r.URL.Path}
			case "query":
				// Check both raw and decoded query
				values = []string{r.URL.RawQuery, strings.ReplaceAll(r.URL.RawQuery, "+", " ")}
			}
			for _, value := range values {
				if value != "" && rule.Pattern.MatchString(value) {
					return rule.Name
				}
			}
		}
	}
	return ""
}

// WAFMiddleware wraps a handler with WAF protection.
func WAFMiddleware(waf *WAF, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matched := waf.Check(r); matched != "" {
			cdnlog.Warn("WAF blocked request",
				"rule", matched,
				"ip", clientIP(r),
				"uri", r.URL.RequestURI(),
			)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AddRule adds a custom WAF rule.
func (w *WAF) AddRule(name, pattern string, targets []string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	w.rules = append(w.rules, WAFRule{Name: name, Pattern: re, Targets: targets})
	return nil
}

// SetEnabled enables or disables the WAF.
func (w *WAF) SetEnabled(enabled bool) {
	w.enabled = enabled
}

// sanitizeForLog removes potentially dangerous characters from log output.
func sanitizeForLog(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
