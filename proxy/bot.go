package proxy

import (
	"net/http"
	"regexp"
	"strings"
	"sync"

	cdnlog "github.com/EdgeFlowCDN/cdn-edge/log"
)

// BotAction defines what to do with detected bots.
type BotAction string

const (
	BotAllow     BotAction = "allow"
	BotBlock     BotAction = "block"
	BotChallenge BotAction = "challenge"
)

// BotRule defines a bot detection rule.
type BotRule struct {
	Name    string
	Pattern *regexp.Regexp
	Action  BotAction
}

// BotDetector identifies and handles bot traffic.
type BotDetector struct {
	mu    sync.RWMutex
	rules []BotRule
}

// NewBotDetector creates a bot detector with default rules.
func NewBotDetector() *BotDetector {
	return &BotDetector{
		rules: []BotRule{
			// Known good bots - allow
			{Name: "googlebot", Pattern: regexp.MustCompile(`(?i)googlebot`), Action: BotAllow},
			{Name: "bingbot", Pattern: regexp.MustCompile(`(?i)bingbot`), Action: BotAllow},
			{Name: "baiduspider", Pattern: regexp.MustCompile(`(?i)baiduspider`), Action: BotAllow},

			// Malicious/aggressive bots - block
			{Name: "sqlmap", Pattern: regexp.MustCompile(`(?i)sqlmap`), Action: BotBlock},
			{Name: "nikto", Pattern: regexp.MustCompile(`(?i)nikto`), Action: BotBlock},
			{Name: "nmap", Pattern: regexp.MustCompile(`(?i)nmap`), Action: BotBlock},
			{Name: "masscan", Pattern: regexp.MustCompile(`(?i)masscan`), Action: BotBlock},
			{Name: "dirbuster", Pattern: regexp.MustCompile(`(?i)(dirbuster|gobuster|dirb)`), Action: BotBlock},
			{Name: "wp-scan", Pattern: regexp.MustCompile(`(?i)wpscan`), Action: BotBlock},

			// Suspicious patterns - challenge
			{Name: "empty-ua", Pattern: regexp.MustCompile(`^$`), Action: BotChallenge},
			{Name: "curl-default", Pattern: regexp.MustCompile(`^curl/`), Action: BotChallenge},
			{Name: "python-requests", Pattern: regexp.MustCompile(`(?i)python-requests`), Action: BotChallenge},
			{Name: "go-http", Pattern: regexp.MustCompile(`^Go-http-client`), Action: BotChallenge},
			{Name: "java-http", Pattern: regexp.MustCompile(`(?i)^java/`), Action: BotChallenge},
		},
	}
}

// AddRule adds a custom bot detection rule.
func (bd *BotDetector) AddRule(name, pattern string, action BotAction) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	bd.mu.Lock()
	bd.rules = append(bd.rules, BotRule{Name: name, Pattern: re, Action: action})
	bd.mu.Unlock()
	return nil
}

// Detect checks the User-Agent and returns the action to take.
func (bd *BotDetector) Detect(userAgent string) (BotAction, string) {
	bd.mu.RLock()
	defer bd.mu.RUnlock()

	for _, rule := range bd.rules {
		if rule.Pattern.MatchString(userAgent) {
			return rule.Action, rule.Name
		}
	}
	return BotAllow, ""
}

// BotMiddleware wraps a handler with bot detection.
func BotMiddleware(bd *BotDetector, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		action, ruleName := bd.Detect(ua)

		switch action {
		case BotBlock:
			cdnlog.Debug("bot blocked", "rule", ruleName, "ua", truncate(ua, 100), "ip", clientIP(r))
			http.Error(w, "Forbidden", http.StatusForbidden)
			return

		case BotChallenge:
			// Check if challenge cookie is present (already verified)
			if cookie, err := r.Cookie("__ef_check"); err == nil && cookie.Value != "" {
				next.ServeHTTP(w, r)
				return
			}
			cdnlog.Debug("bot challenge", "rule", ruleName, "ua", truncate(ua, 100), "ip", clientIP(r))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(ChallengePageHTML()))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
