// Package config loads and saves wgo's TOML configuration and runtime state.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// Default filesystem locations. Variables so tests can override.
var (
	ConfigPath = "/etc/wgo/config.toml"
	StatePath  = "/var/lib/wgo/state.json"
)

// Global holds app-wide settings.
type Global struct {
	QuitDefault           string   `toml:"quit_default"`
	HandshakeStaleSeconds int      `toml:"handshake_stale_seconds"`
	StageTimeoutSeconds   int      `toml:"stage_timeout_seconds"`
	DNSCheckHost          string   `toml:"dns_check_host"`
	ProbeTarget           string   `toml:"probe_target"`
	PublicIPURL           string   `toml:"public_ip_url"`
	LANRanges             []string `toml:"lan_ranges"`
	Killswitch            bool     `toml:"killswitch"`
	AllowLAN              bool     `toml:"allow_lan"`
	AutoReconnect         bool     `toml:"auto_reconnect"`
	ReconnectMaxAttempts  int      `toml:"reconnect_max_attempts"`
}

// Profile holds per-profile overrides. Pointer fields fall back to Global.
type Profile struct {
	Killswitch    *bool   `toml:"killswitch,omitempty"`
	AllowLAN      *bool   `toml:"allow_lan,omitempty"`
	AutoReconnect *bool   `toml:"auto_reconnect,omitempty"`
	ProbeTarget   *string `toml:"probe_target,omitempty"`
	DNSCheckHost  *string `toml:"dns_check_host,omitempty"`
}

// Config is the full application configuration.
type Config struct {
	Global   Global             `toml:"global"`
	Profiles map[string]Profile `toml:"profiles"`
}

// Defaults returns the built-in default configuration.
func Defaults() *Config {
	return &Config{
		Global: Global{
			QuitDefault:           "dynamic",
			HandshakeStaleSeconds: 180,
			StageTimeoutSeconds:   15,
			DNSCheckHost:          "example.com",
			ProbeTarget:           "1.1.1.1",
			PublicIPURL:           "https://api.ipify.org",
			LANRanges:             []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			Killswitch:            false,
			AllowLAN:              true,
			AutoReconnect:         false,
			ReconnectMaxAttempts:  5,
		},
		Profiles: map[string]Profile{},
	}
}

// Load reads ConfigPath, creating it with defaults on first run.
// Missing fields fall back to defaults.
func Load() (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		if werr := cfg.Save(); werr != nil {
			// Non-fatal: still usable with in-memory defaults.
			return cfg, nil
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return Defaults(), fmt.Errorf("parse %s: %w", ConfigPath, err)
	}
	cfg.fillZeroDefaults()
	return cfg, nil
}

// Parse decodes TOML bytes over defaults (exposed for tests).
func Parse(data []byte) (*Config, error) {
	cfg := Defaults()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.fillZeroDefaults()
	return cfg, nil
}

func (c *Config) fillZeroDefaults() {
	d := Defaults().Global
	g := &c.Global
	if g.QuitDefault == "" {
		g.QuitDefault = d.QuitDefault
	}
	if g.HandshakeStaleSeconds <= 0 {
		g.HandshakeStaleSeconds = d.HandshakeStaleSeconds
	}
	if g.StageTimeoutSeconds <= 0 {
		g.StageTimeoutSeconds = d.StageTimeoutSeconds
	}
	if g.DNSCheckHost == "" {
		g.DNSCheckHost = d.DNSCheckHost
	}
	if g.ProbeTarget == "" {
		g.ProbeTarget = d.ProbeTarget
	}
	if g.PublicIPURL == "" {
		g.PublicIPURL = d.PublicIPURL
	}
	if len(g.LANRanges) == 0 {
		g.LANRanges = d.LANRanges
	}
	if g.ReconnectMaxAttempts <= 0 {
		g.ReconnectMaxAttempts = d.ReconnectMaxAttempts
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
}

// Save writes the configuration to ConfigPath, creating directories.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath, data, 0o644)
}

// Effective is the resolved per-profile view of the configuration.
type Effective struct {
	Killswitch    bool
	AllowLAN      bool
	AutoReconnect bool
	ProbeTarget   string
	DNSCheckHost  string
}

// Resolve merges global defaults with per-profile overrides for name.
func (c *Config) Resolve(name string) Effective {
	e := Effective{
		Killswitch:    c.Global.Killswitch,
		AllowLAN:      c.Global.AllowLAN,
		AutoReconnect: c.Global.AutoReconnect,
		ProbeTarget:   c.Global.ProbeTarget,
		DNSCheckHost:  c.Global.DNSCheckHost,
	}
	p, ok := c.Profiles[name]
	if !ok {
		return e
	}
	if p.Killswitch != nil {
		e.Killswitch = *p.Killswitch
	}
	if p.AllowLAN != nil {
		e.AllowLAN = *p.AllowLAN
	}
	if p.AutoReconnect != nil {
		e.AutoReconnect = *p.AutoReconnect
	}
	if p.ProbeTarget != nil && *p.ProbeTarget != "" {
		e.ProbeTarget = *p.ProbeTarget
	}
	if p.DNSCheckHost != nil && *p.DNSCheckHost != "" {
		e.DNSCheckHost = *p.DNSCheckHost
	}
	return e
}

// SetProfile stores a per-profile override set.
func (c *Config) SetProfile(name string, p Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
}
