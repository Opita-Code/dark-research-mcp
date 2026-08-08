// Package config loads and validates the dark-research configuration.
//
// File path precedence (highest wins):
//  1. --config flag or $DARK_RESEARCH_CONFIG
//  2. ./dark-research.toml
//  3. ~/.config/dark-research/config.toml (platform-specific via os.UserConfigDir)
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Network  NetworkConfig  `toml:"network"`
	Tor      TorConfig      `toml:"tor"`
	Safety   SafetyConfig   `toml:"safety"`
	Risk     RiskConfig     `toml:"risk"`
	Research ResearchConfig `toml:"research"`
}

type NetworkConfig struct {
	RequestTimeoutSecs int `toml:"request_timeout_secs"`
	MaxConcurrent      int `toml:"max_concurrent"`
}

type TorConfig struct {
	// Mode is "embedded" or "external". The Go implementation uses
	// external (SOCKS5) by default since libtor via CGo is heavy.
	Mode string `toml:"mode"`
	// SOCKS5URL is used when Mode == "external". Example: "socks5://127.0.0.1:9150".
	SOCKS5URL string `toml:"socks5_url"`
}

type SafetyConfig struct {
	BlockPrivateIPs  bool `toml:"block_private_ips"`
	AllowLoopback    bool `toml:"allow_loopback"`
	MaxResponseBytes int  `toml:"max_response_bytes"`
	MaxOutputChars   int  `toml:"max_tool_output_chars"`
}

type RiskConfig struct {
	// RequireConsentFor lists tool names whose invocation needs explicit user consent.
	RequireConsentFor []string `toml:"require_consent_for"`
}

// ResearchConfig controls the v0.8.1 router features (persistence-aware
// recall cache + optional LLM synthesis).
//
// EnableCache is ON by default: within-TTL recall of a prior run for the
// same (query, intent) saves backend rate-limit consumption (ip-api
// 45 req/min, NVD 5 req/30s) and avoids duplicate HTTP. Cache misses
// fall through to the normal multi-backend fan-out, so enabling it is
// low-risk.
//
// EnableSynthesize is OFF by default: each synthesized result costs one
// LLM call per query. Operators opt in via TOML (research.enable_synthesize)
// or the DARK_RESEARCH_ENABLE_SYNTHESIZE env override.
type ResearchConfig struct {
	EnableCache      bool `toml:"enable_cache"`
	EnableSynthesize bool `toml:"enable_synthesize"`
}

// Defaults returns the platform-default configuration.
func Defaults() Config {
	return Config{
		Network: NetworkConfig{
			RequestTimeoutSecs: 30,
			MaxConcurrent:      4,
		},
		Tor: TorConfig{
			Mode:      "external",
			SOCKS5URL: "socks5://127.0.0.1:9150",
		},
		Safety: SafetyConfig{
			BlockPrivateIPs:  true,
			AllowLoopback:    false,
			MaxResponseBytes: 5_000_000,
			MaxOutputChars:   200_000,
		},
		Risk: RiskConfig{
			RequireConsentFor: []string{"onion_fetch", "email_osint"},
		},
		Research: ResearchConfig{
			EnableCache:      true,  // persistence-aware recall: cheap win, saves rate limits
			EnableSynthesize: false, // LLM call per query: opt-in
		},
	}
}

// Load reads config from path. If path is empty, walks the precedence chain.
func Load(path string) (Config, error) {
	cfg := Defaults()
	resolved, err := resolvePath(path)
	if err != nil {
		return cfg, err
	}
	if resolved == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", resolved, err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", resolved, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func resolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	candidates := []string{
		"dark-research.toml",
	}
	if v := os.Getenv("DARK_RESEARCH_CONFIG"); v != "" {
		candidates = append([]string{v}, candidates...)
	}
	if cfgDir, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(cfgDir, "dark-research", "config.toml"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", nil
}

// Validate enforces invariants the TOML alone cannot express.
func (c Config) Validate() error {
	if c.Network.RequestTimeoutSecs <= 0 {
		return errors.New("network.request_timeout_secs must be > 0")
	}
	if c.Network.RequestTimeoutSecs > 600 {
		return errors.New("network.request_timeout_secs > 600s is unreasonable")
	}
	if c.Network.MaxConcurrent <= 0 || c.Network.MaxConcurrent > 64 {
		return errors.New("network.max_concurrent must be in 1..64")
	}
	switch c.Tor.Mode {
	case "embedded", "external":
	default:
		return fmt.Errorf("tor.mode must be 'embedded' or 'external', got %q", c.Tor.Mode)
	}
	if c.Tor.Mode == "external" && c.Tor.SOCKS5URL == "" {
		return errors.New("tor.socks5_url required when tor.mode = 'external'")
	}
	if c.Safety.MaxResponseBytes <= 0 {
		return errors.New("safety.max_response_bytes must be > 0")
	}
	if c.Safety.MaxOutputChars <= 0 {
		return errors.New("safety.max_tool_output_chars must be > 0")
	}
	return nil
}

// RequestTimeout returns the configured HTTP timeout as a time.Duration.
func (c Config) RequestTimeout() time.Duration {
	return time.Duration(c.Network.RequestTimeoutSecs) * time.Second
}
