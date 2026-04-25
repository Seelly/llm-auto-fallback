package config

import (
	"log"
	"os"
	"regexp"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig     `yaml:"server"`
	Providers []ProviderConfig `yaml:"providers"`
	Fallback  FallbackConfig   `yaml:"fallback"`
	Probe     ProbeConfig      `yaml:"probe"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type ProviderConfig struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"baseurl"`
	APIKey  string `yaml:"api_key"`
}

type FallbackConfig struct {
	Custom         []string `yaml:"custom"`
	GlobalPriority []string `yaml:"global_priority"`
}

type ProbeConfig struct {
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	CacheTTL time.Duration `yaml:"cache_ttl"`
}

var envVarRe = regexp.MustCompile(`\$\{(\w+)\}`)

func Load(path string) (*Config, error) {
	// Load .env file if exists (silent fail if not found)
	if err := godotenv.Load(); err == nil {
		log.Println("[config] loaded .env file")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	// First parse YAML to get raw config
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Apply environment variable expansion with priority:
	// 1. Environment variable (highest)
	// 2. .env file (loaded by godotenv)
	// 3. YAML config value (lowest)
	cfg.expandEnvVars()
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) expandEnvVars() {
	for i := range c.Providers {
		c.Providers[i].APIKey = expandEnvVar(c.Providers[i].APIKey)
		c.Providers[i].BaseURL = expandEnvVar(c.Providers[i].BaseURL)
	}
}

func expandEnvVar(value string) string {
	return envVarRe.ReplaceAllStringFunc(value, func(match string) string {
		// match looks like ${VAR_NAME}
		varName := match[2 : len(match)-1]
		if envVal := os.Getenv(varName); envVal != "" {
			return envVal
		}
		// If env var not set, keep original value (could be literal in YAML)
		return match
	})
}

func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Probe.Interval == 0 {
		c.Probe.Interval = 30 * time.Second
	}
	if c.Probe.Timeout == 0 {
		c.Probe.Timeout = 10 * time.Second
	}
	if c.Probe.CacheTTL == 0 {
		c.Probe.CacheTTL = 60 * time.Second
	}
	if len(c.Fallback.GlobalPriority) == 0 {
		for _, p := range c.Providers {
			c.Fallback.GlobalPriority = append(c.Fallback.GlobalPriority, p.Name)
		}
	}
}

// ProviderByName returns the provider config by name.
func (c *Config) ProviderByName(name string) *ProviderConfig {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i]
		}
	}
	return nil
}
