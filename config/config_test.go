package config

import (
	"os"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"512MB", 512 * 1024 * 1024},
		{"1GB", 1024 * 1024 * 1024},
		{"50GB", 50 * 1024 * 1024 * 1024},
		{"1KB", 1024},
		{"100B", 100},
		{"1024", 1024},
		{"1mb", 1024 * 1024},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if err != nil {
			t.Errorf("ParseSize(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestParseSizeError(t *testing.T) {
	_, err := ParseSize("")
	if err == nil {
		t.Error("expected error for empty string")
	}
	_, err = ParseSize("abc")
	if err == nil {
		t.Error("expected error for invalid string")
	}
}

func TestLoadConfig(t *testing.T) {
	yaml := `
server:
  listen: ":9090"

cache:
  memory:
    max_size: "256MB"
    max_object_size: "512KB"
  disk:
    path: "/tmp/test-cache"
    max_size: "5GB"

log:
  level: "debug"

domains:
  - host: "test.example.com"
    origins:
      - addr: "https://origin.example.com"
        weight: 100
    cache:
      default_ttl: "5m"
      ignore_query: true
`
	f, err := os.CreateTemp("", "edge-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Listen != ":9090" {
		t.Errorf("Listen = %q, want %q", cfg.Server.Listen, ":9090")
	}
	if cfg.Cache.Memory.MaxSize != "256MB" {
		t.Errorf("Memory.MaxSize = %q, want %q", cfg.Cache.Memory.MaxSize, "256MB")
	}
	if len(cfg.Domains) != 1 {
		t.Fatalf("len(Domains) = %d, want 1", len(cfg.Domains))
	}
	if cfg.Domains[0].Host != "test.example.com" {
		t.Errorf("Domain host = %q, want %q", cfg.Domains[0].Host, "test.example.com")
	}
	if !cfg.Domains[0].Cache.IgnoreQuery {
		t.Error("IgnoreQuery should be true")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	// No domains
	yaml := `
server:
  listen: ":80"
domains: []
`
	f, _ := os.CreateTemp("", "edge-config-*.yaml")
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	_, err := Load(f.Name())
	if err == nil {
		t.Error("expected validation error for empty domains")
	}
}
