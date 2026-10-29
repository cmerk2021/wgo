package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	g := c.Global
	if g.QuitDefault != "dynamic" || g.HandshakeStaleSeconds != 180 ||
		g.StageTimeoutSeconds != 15 || g.DNSCheckHost != "example.com" ||
		g.ProbeTarget != "1.1.1.1" || g.PublicIPURL != "https://api.ipify.org" {
		t.Errorf("unexpected defaults: %+v", g)
	}
	if len(g.LANRanges) != 3 {
		t.Errorf("expected 3 default LAN ranges, got %v", g.LANRanges)
	}
	if g.Killswitch {
		t.Error("kill switch must default to off")
	}
}

func TestParseOverridesAndFallback(t *testing.T) {
	data := []byte(`
[global]
handshake_stale_seconds = 60
probe_target = "9.9.9.9"

[profiles.home]
killswitch = true
allow_lan = true
auto_reconnect = true

[profiles.work]
killswitch = false
probe_target = "10.0.0.1"
`)
	c, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.Global.HandshakeStaleSeconds != 60 {
		t.Errorf("override lost: %d", c.Global.HandshakeStaleSeconds)
	}
	// Unspecified globals fall back to defaults.
	if c.Global.StageTimeoutSeconds != 15 || c.Global.DNSCheckHost != "example.com" {
		t.Errorf("defaults not filled: %+v", c.Global)
	}

	home := c.Resolve("home")
	if !home.Killswitch || !home.AllowLAN || !home.AutoReconnect {
		t.Errorf("home profile overrides wrong: %+v", home)
	}
	if home.ProbeTarget != "9.9.9.9" {
		t.Errorf("home should inherit global probe target, got %s", home.ProbeTarget)
	}

	work := c.Resolve("work")
	if work.Killswitch {
		t.Error("work killswitch should be false")
	}
	if work.ProbeTarget != "10.0.0.1" {
		t.Errorf("work probe target override lost: %s", work.ProbeTarget)
	}

	// Unknown profile falls back entirely to globals.
	other := c.Resolve("nope")
	if other.Killswitch || other.ProbeTarget != "9.9.9.9" {
		t.Errorf("unknown profile fallback wrong: %+v", other)
	}
}

func TestParseInvalidTOML(t *testing.T) {
	if _, err := Parse([]byte("not [ valid")); err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadCreatesDefaultFile(t *testing.T) {
	dir := t.TempDir()
	oldCfg := ConfigPath
	ConfigPath = filepath.Join(dir, "sub", "config.toml")
	defer func() { ConfigPath = oldCfg }()

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Global.DNSCheckHost != "example.com" {
		t.Errorf("unexpected config: %+v", c.Global)
	}
	if _, err := os.Stat(ConfigPath); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	oldState := StatePath
	StatePath = filepath.Join(dir, "state.json")
	defer func() { StatePath = oldState }()

	st, err := LoadState()
	if err != nil || st.LastProfile != "" {
		t.Fatalf("fresh state: %v %+v", err, st)
	}
	st.LastProfile = "home"
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := LoadState()
	if err != nil || st2.LastProfile != "home" {
		t.Fatalf("round trip failed: %v %+v", err, st2)
	}
}
