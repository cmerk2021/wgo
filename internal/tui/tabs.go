package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmerk2021/wgo/internal/config"
)

func (m *Model) handleProfilesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up":
		if m.profCursor > 0 {
			m.profCursor--
		}
	case "down", "j":
		if m.profCursor < len(m.profiles)-1 {
			m.profCursor++
		}
	case "enter":
		if m.profCursor < len(m.profiles) &&
			(m.connState == StateIdle || m.connState == StateFailed) {
			p := m.profiles[m.profCursor]
			return m, m.startConnect(&p)
		}
		if m.connState != StateIdle {
			m.flash = "disconnect first (c) before switching profiles"
		}
	case "i":
		m.importing = true
		m.importInput.SetValue("")
		return m, m.importInput.Focus()
	case "e":
		if m.profCursor < len(m.profiles) {
			return m, m.editProfile(m.profiles[m.profCursor])
		}
	case "w":
		m.toggleProfileBool(func(p *config.Profile, cur bool) { v := !cur; p.Killswitch = &v },
			func(e config.Effective) bool { return e.Killswitch })
	case "a":
		m.toggleProfileBool(func(p *config.Profile, cur bool) { v := !cur; p.AutoReconnect = &v },
			func(e config.Effective) bool { return e.AutoReconnect })
	}
	// 'l' doubles as cursor-down above; use 'L' for allow-LAN toggle instead.
	if key == "L" {
		m.toggleProfileBool(func(p *config.Profile, cur bool) { v := !cur; p.AllowLAN = &v },
			func(e config.Effective) bool { return e.AllowLAN })
	}
	return m, nil
}

func (m *Model) toggleProfileBool(set func(*config.Profile, bool), get func(config.Effective) bool) {
	if m.profCursor >= len(m.profiles) {
		return
	}
	name := m.profiles[m.profCursor].Name
	eff := m.cfg.Resolve(name)
	p := m.cfg.Profiles[name]
	set(&p, get(eff))
	m.cfg.SetProfile(name, p)
	if err := m.cfg.Save(); err != nil {
		m.flash = "config save: " + err.Error()
		return
	}
	m.addLog("updated settings for profile " + name)
}

func (m *Model) viewProfiles() string {
	var b strings.Builder
	b.WriteString("\n")
	if m.importing {
		b.WriteString("  Import .conf from path (enter to confirm, esc to cancel):\n")
		b.WriteString("  " + m.importInput.View() + "\n\n")
	}
	if len(m.profiles) == 0 {
		b.WriteString(styleLabel.Render("  no profiles found in /etc/wireguard — press i to import one\n"))
		return b.String()
	}
	for i, p := range m.profiles {
		cursor := "  "
		if i == m.profCursor {
			cursor = styleSelected.Render("> ")
		}
		name := p.Name
		var marks []string
		if p.Name == m.activeIface && (m.connState == StateConnected || m.connState == StateStale || m.connState == StateConnecting || m.connState == StateReconnecting) {
			marks = append(marks, styleOK.Render("active"))
		}
		if p.Name == m.state.LastProfile {
			marks = append(marks, styleLabel.Render("last used"))
		}
		if p.ParseErr != nil {
			marks = append(marks, styleFail.Render("invalid: "+p.ParseErr.Error()))
		} else {
			eff := m.cfg.Resolve(p.Name)
			var opts []string
			if eff.Killswitch {
				opts = append(opts, "ks")
			}
			if eff.AllowLAN {
				opts = append(opts, "lan")
			}
			if eff.AutoReconnect {
				opts = append(opts, "auto-rc")
			}
			if len(opts) > 0 {
				marks = append(marks, styleLabel.Render("["+strings.Join(opts, " ")+"]"))
			}
			if ep := p.EndpointHost(); ep != "" {
				marks = append(marks, styleLabel.Render(ep))
			}
			if len(p.Peers) == 0 {
				marks = append(marks, styleFail.Render("no [Peer]"))
			}
		}
		line := fmt.Sprintf("%s%s  %s", cursor, styleValue.Render(fmt.Sprintf("%-16s", name)), strings.Join(marks, "  "))
		b.WriteString(truncate(line, m.width-2) + "\n")
	}
	b.WriteString("\n" + styleLabel.Render("  w: kill switch · L: allow LAN · a: auto-reconnect (per profile, saved to config)") + "\n")
	return b.String()
}

func (m *Model) viewThroughput() string {
	var b strings.Builder
	b.WriteString("\n")
	rxSeries, txSeries := m.rates.RateSeries()
	w := m.width - 14
	if w < 10 {
		w = 10
	}
	rxRate, txRate := m.rates.Rates()
	peakRx, peakTx := m.rates.Peaks()
	b.WriteString("  " + styleLabel.Render("RX ") + styleOK.Render(Sparkline(rxSeries, w)) + "\n")
	b.WriteString(fmt.Sprintf("     %s now · %s peak\n\n",
		styleValue.Render(HumanRate(rxRate)), styleLabel.Render(HumanRate(peakRx))))
	b.WriteString("  " + styleLabel.Render("TX ") + styleConnecting.Render(Sparkline(txSeries, w)) + "\n")
	b.WriteString(fmt.Sprintf("     %s now · %s peak\n\n",
		styleValue.Render(HumanRate(txRate)), styleLabel.Render(HumanRate(peakTx))))
	if m.stats != nil {
		b.WriteString(fmt.Sprintf("  %s ↓ %s   ↑ %s\n",
			styleLabel.Render("Session totals"),
			styleValue.Render(HumanBytes(m.stats.TotalRx())),
			styleValue.Render(HumanBytes(m.stats.TotalTx()))))
	} else {
		b.WriteString(styleLabel.Render("  not connected — no data\n"))
	}
	b.WriteString("\n" + styleLabel.Render(fmt.Sprintf("  rolling window: last %d samples (~120s)", 120)) + "\n")
	return b.String()
}

func (m *Model) viewLogs() string {
	if !m.logReady {
		return "\n  (no logs yet)\n"
	}
	return m.logView.View()
}
