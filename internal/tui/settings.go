package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type settingItem struct {
	label  string
	get    func(m *Model) string
	isBool bool
	toggle func(m *Model)
	set    func(m *Model, v string) error
}

var settingItems = []settingItem{
	{
		label: "Default kill switch",
		get:   func(m *Model) string { return boolStr(m.cfg.Global.Killswitch) },
		isBool: true,
		toggle: func(m *Model) { m.cfg.Global.Killswitch = !m.cfg.Global.Killswitch },
	},
	{
		label: "Allow LAN (default)",
		get:   func(m *Model) string { return boolStr(m.cfg.Global.AllowLAN) },
		isBool: true,
		toggle: func(m *Model) { m.cfg.Global.AllowLAN = !m.cfg.Global.AllowLAN },
	},
	{
		label: "Auto-reconnect (default)",
		get:   func(m *Model) string { return boolStr(m.cfg.Global.AutoReconnect) },
		isBool: true,
		toggle: func(m *Model) { m.cfg.Global.AutoReconnect = !m.cfg.Global.AutoReconnect },
	},
	{
		label: "LAN ranges",
		get:   func(m *Model) string { return strings.Join(m.cfg.Global.LANRanges, ", ") },
		set: func(m *Model, v string) error {
			var out []string
			for _, p := range strings.Split(v, ",") {
				if t := strings.TrimSpace(p); t != "" {
					out = append(out, t)
				}
			}
			m.cfg.Global.LANRanges = out
			return nil
		},
	},
	{
		label: "Handshake stale threshold (s)",
		get:   func(m *Model) string { return strconv.Itoa(m.cfg.Global.HandshakeStaleSeconds) },
		set: func(m *Model, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return fmt.Errorf("must be a positive integer")
			}
			m.cfg.Global.HandshakeStaleSeconds = n
			return nil
		},
	},
	{
		label: "Stage timeout (s)",
		get:   func(m *Model) string { return strconv.Itoa(m.cfg.Global.StageTimeoutSeconds) },
		set: func(m *Model, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return fmt.Errorf("must be a positive integer")
			}
			m.cfg.Global.StageTimeoutSeconds = n
			return nil
		},
	},
	{
		label: "DNS check host",
		get:   func(m *Model) string { return m.cfg.Global.DNSCheckHost },
		set:   func(m *Model, v string) error { m.cfg.Global.DNSCheckHost = strings.TrimSpace(v); return nil },
	},
	{
		label: "Probe target",
		get:   func(m *Model) string { return m.cfg.Global.ProbeTarget },
		set:   func(m *Model, v string) error { m.cfg.Global.ProbeTarget = strings.TrimSpace(v); return nil },
	},
	{
		label: "Public IP URL",
		get:   func(m *Model) string { return m.cfg.Global.PublicIPURL },
		set:   func(m *Model, v string) error { m.cfg.Global.PublicIPURL = strings.TrimSpace(v); return nil },
	},
	{
		label: "Reconnect max attempts",
		get:   func(m *Model) string { return strconv.Itoa(m.cfg.Global.ReconnectMaxAttempts) },
		set: func(m *Model, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return fmt.Errorf("must be a positive integer")
			}
			m.cfg.Global.ReconnectMaxAttempts = n
			return nil
		},
	},
}

func boolStr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m *Model) handleSettingsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "j":
		if m.settingsCursor < len(settingItems)-1 {
			m.settingsCursor++
		}
	case "enter":
		item := settingItems[m.settingsCursor]
		if item.isBool {
			item.toggle(m)
			m.saveConfig()
			return m, nil
		}
		m.settingsEdit = true
		m.settingsInput.SetValue(item.get(m))
		return m, m.settingsInput.Focus()
	}
	return m, nil
}

func (m *Model) applySettingEdit() {
	item := settingItems[m.settingsCursor]
	if item.set == nil {
		return
	}
	if err := item.set(m, m.settingsInput.Value()); err != nil {
		m.flash = item.label + ": " + err.Error()
		return
	}
	m.saveConfig()
}

func (m *Model) saveConfig() {
	if err := m.cfg.Save(); err != nil {
		m.flash = "config save: " + err.Error()
		return
	}
	m.addLog("settings saved to /etc/wgo/config.toml")
}

func (m *Model) viewSettings() string {
	var b strings.Builder
	b.WriteString("\n")
	for i, item := range settingItems {
		cursor := "  "
		if i == m.settingsCursor {
			cursor = styleSelected.Render("> ")
		}
		val := item.get(m)
		if m.settingsEdit && i == m.settingsCursor {
			val = m.settingsInput.View()
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor,
			styleLabel.Render(fmt.Sprintf("%-30s", item.label)), styleValue.Render(val)))
	}
	b.WriteString("\n" + styleLabel.Render("  changes are saved to /etc/wgo/config.toml immediately") + "\n")
	return b.String()
}
