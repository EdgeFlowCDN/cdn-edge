package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig   `yaml:"server"`
	Cache   CacheConfig    `yaml:"cache"`
	Log     LogConfig      `yaml:"log"`
	Domains []DomainConfig `yaml:"domains"`
}

type ServerConfig struct {
	Listen        string `yaml:"listen"`
	ListenHTTPS   string `yaml:"listen_https"`
	MetricsListen string `yaml:"metrics_listen"`
}

type CacheConfig struct {
	Memory MemoryCacheConfig `yaml:"memory"`
	Disk   DiskCacheConfig   `yaml:"disk"`
}

type MemoryCacheConfig struct {
	MaxSize       string `yaml:"max_size"`
	MaxObjectSize string `yaml:"max_object_size"`
}

type DiskCacheConfig struct {
	Path          string `yaml:"path"`
	MaxSize       string `yaml:"max_size"`
	MaxObjectSize string `yaml:"max_object_size"`
}

type LogConfig struct {
	AccessLog string `yaml:"access_log"`
	ErrorLog  string `yaml:"error_log"`
	Level     string `yaml:"level"`
}

type DomainConfig struct {
	Host    string            `yaml:"host"`
	Origins []OriginConfig    `yaml:"origins"`
	Cache   DomainCacheConfig `yaml:"cache"`
}

type OriginConfig struct {
	Addr     string `yaml:"addr"`
	Weight   int    `yaml:"weight"`
	Priority int    `yaml:"priority"` // 0=primary, 1+=backup
}

type DomainCacheConfig struct {
	DefaultTTL  string `yaml:"default_ttl"`
	IgnoreQuery bool   `yaml:"ignore_query"`
	ForceTTL    string `yaml:"force_ttl"`
}

// Load reads and parses a YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Server.Listen == "" {
		c.Server.Listen = ":80"
	}
	if len(c.Domains) == 0 {
		return fmt.Errorf("at least one domain must be configured")
	}
	for i, d := range c.Domains {
		if d.Host == "" {
			return fmt.Errorf("domain[%d]: host is required", i)
		}
		if len(d.Origins) == 0 {
			return fmt.Errorf("domain[%d] (%s): at least one origin is required", i, d.Host)
		}
		for j, o := range d.Origins {
			if o.Addr == "" {
				return fmt.Errorf("domain[%d] (%s) origin[%d]: addr is required", i, d.Host, j)
			}
			if o.Weight <= 0 {
				c.Domains[i].Origins[j].Weight = 100
			}
		}
		if d.Cache.DefaultTTL == "" {
			c.Domains[i].Cache.DefaultTTL = "10m"
		}
	}
	if c.Cache.Memory.MaxSize == "" {
		c.Cache.Memory.MaxSize = "512MB"
	}
	if c.Cache.Memory.MaxObjectSize == "" {
		c.Cache.Memory.MaxObjectSize = "1MB"
	}
	if c.Cache.Disk.MaxSize == "" {
		c.Cache.Disk.MaxSize = "50GB"
	}
	if c.Cache.Disk.MaxObjectSize == "" {
		c.Cache.Disk.MaxObjectSize = "500MB"
	}
	if c.Cache.Disk.Path == "" {
		c.Cache.Disk.Path = "/data/cache"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	return nil
}

// ParseSize converts a human-readable size string (e.g., "512MB", "50GB") to bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Order matters: check longer suffixes first
	type sizeUnit struct {
		suffix string
		mult   int64
	}
	units := []sizeUnit{
		{"TB", 1024 * 1024 * 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	s = strings.ToUpper(s)
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			numStr := strings.TrimSuffix(s, u.suffix)
			num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size number %q: %w", numStr, err)
			}
			return int64(num * float64(u.mult)), nil
		}
	}

	// Try parsing as plain number (bytes)
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size string %q", s)
	}
	return num, nil
}

// ParseDuration parses a duration string, supporting Go's time.ParseDuration format.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}
