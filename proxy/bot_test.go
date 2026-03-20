package proxy

import "testing"

func TestBotDetector(t *testing.T) {
	bd := NewBotDetector()

	tests := []struct {
		ua     string
		action BotAction
	}{
		{"Mozilla/5.0 (compatible; Googlebot/2.1)", BotAllow},
		{"sqlmap/1.0", BotBlock},
		{"nikto/2.0", BotBlock},
		{"", BotChallenge},
		{"curl/7.68", BotChallenge},
		{"python-requests/2.25", BotChallenge},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120", BotAllow},
	}

	for _, tt := range tests {
		action, _ := bd.Detect(tt.ua)
		if action != tt.action {
			t.Errorf("Detect(%q) = %s, want %s", tt.ua, action, tt.action)
		}
	}
}

func TestBotDetectorCustomRule(t *testing.T) {
	bd := NewBotDetector()
	bd.AddRule("custom-bot", "MyCustomBot", BotBlock)

	action, name := bd.Detect("MyCustomBot/1.0")
	if action != BotBlock || name != "custom-bot" {
		t.Errorf("custom rule: action=%s name=%s, want block/custom-bot", action, name)
	}
}
