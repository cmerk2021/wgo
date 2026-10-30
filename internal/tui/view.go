package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/cmerk2021/wgo/internal/monitor"
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.width < 40 || m.height < 12 {
		return "\n  terminal too small for wgo\n  please resize (need at least 40x12)\n"
	}
	if m.showHelp {
		return m.viewHelp()
	}

	var body string
	switch m.tab {
	case TabDashboard:
		body = m.viewDashboard()
	case TabProfiles:
		body = m.viewProfiles()
	case TabThroughput:
		body = m.viewThroughput()
	case TabLogs:
		body = m.viewLogs()
	case TabSettings:
		body = m.viewSettings()
	}

	header := m.viewTabs()
	footer := m.viewFooter()

	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body = lipgloss.NewStyle().Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) viewTabs() string {
	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if Tab(i) == m.tab {
			tabs = append(tabs, styleTabActive.Render(label))
		} else {
			tabs = append(tabs, styleTab.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)
}

func (m *Model) viewFooter() string {
	if m.confirmQuit {
		return styleError.Render("Quitting will remove the kill switch. The tunnel stays up. Quit anyway? [y/N]")
	}
	if m.confirmOver != "" {
		return styleError.Render("Profile already exists. Overwrite? [y/N]")
	}
	if m.flash != "" {
		return styleError.Render(m.flash)
	}
	hints := "c connect/disconnect · k kill switch · t leak test · d disconnect+quit · q quit · ? help"
	switch m.tab {
	case TabProfiles:
		hints = "↑/↓ select · enter connect · i import · e edit · w ks · l lan · a auto-rc · " + hints
	case TabSettings:
		hints = "↑/↓ select · enter edit/toggle · " + hints
	case TabLogs:
		hints = "↑/↓ scroll · " + hints
	}
	return styleFooter.Render(truncate(hints, m.width))
}

func (m *Model) viewDashboard() string {
	var b strings.Builder
	b.WriteString("\n")

	// Big status indicator.
	var status string
	switch m.connState {
	case StateConnected:
		status = styleConnected.Render("● CONNECTED")
	case StateConnecting:
		status = styleConnecting.Render(m.spin.View() + "◐ CONNECTING")
	case StateReconnecting:
		status = styleConnecting.Render(m.spin.View() + fmt.Sprintf("◐ RECONNECTING (attempt %d/%d)", m.reconnAttempts, m.cfg.Global.ReconnectMaxAttempts))
	case StateStale:
		status = styleStale.Render("◐ STALE — no recent handshake")
	case StateFailed:
		status = styleError.Render("✗ CONNECTION FAILED")
	case StateDisconnecting:
		status = styleConnecting.Render(m.spin.View() + "◌ DISCONNECTING")
	default:
		status = styleDisconnected.Render("○ DISCONNECTED")
	}
	b.WriteString("  " + status + "\n\n")

	// Staged checklist during connecting / after failure.
	if m.connState == StateConnecting || m.connState == StateFailed || m.connState == StateReconnecting {
		for _, st := range m.stages {
			icon := styleLabel.Render("·")
			detail := ""
			switch {
			case st.running:
				icon = styleConnecting.Render(m.spin.View())
			case st.done && st.ok:
				icon = styleOK.Render("✓")
				detail = styleLabel.Render("  " + st.detail)
			case st.done && !st.ok:
				icon = styleFail.Render("✗")
				detail = styleFail.Render("  " + st.detail)
			}
			b.WriteString(fmt.Sprintf("    %s %s%s\n", icon, st.stage.String(), detail))
		}
		if m.connState == StateFailed {
			b.WriteString("\n" + styleError.Render("    press r to retry, x to tear down") + "\n")
			if m.failOutput != "" {
				b.WriteString(styleLabel.Render(indent(truncateLines(m.failOutput, 6), 4)) + "\n")
			}
		}
		b.WriteString("\n")
	}

	// Key stats.
	if m.connState == StateConnected || m.connState == StateStale || m.connState == StateReconnecting {
		rows := [][2]string{}
		profName := ""
		if m.activeProf != nil {
			profName = m.activeProf.Name
		}
		rows = append(rows, [2]string{"Profile", profName})
		rows = append(rows, [2]string{"Interface", m.activeIface})
		endpoint, tunIP, hsAge, rx, tx := "—", "—", "never", "0 B", "0 B"
		if m.stats != nil {
			if ep := m.stats.Endpoint(); ep != "" {
				endpoint = ep
			}
			if hs := m.stats.LatestHandshake(); !hs.IsZero() {
				hsAge = time.Since(hs).Round(time.Second).String() + " ago"
			}
			rx = HumanBytes(m.stats.TotalRx())
			tx = HumanBytes(m.stats.TotalTx())
		}
		if m.activeProf != nil && len(m.activeProf.Addresses) > 0 {
			tunIP = strings.Join(m.activeProf.Addresses, ", ")
		}
		rows = append(rows, [2]string{"Endpoint", endpoint})
		rows = append(rows, [2]string{"Tunnel IP", tunIP})
		rows = append(rows, [2]string{"Uptime", time.Since(m.connectedAt).Round(time.Second).String()})
		rows = append(rows, [2]string{"Handshake", hsAge})
		rxRate, txRate := m.rates.Rates()
		rows = append(rows, [2]string{"Transfer", fmt.Sprintf("↓ %s  ↑ %s", rx, tx)})
		rows = append(rows, [2]string{"Rate", fmt.Sprintf("↓ %s  ↑ %s", HumanRate(rxRate), HumanRate(txRate))})
		ks := styleLabel.Render("off")
		if m.ctrl.KillswitchActive() {
			ks = styleOK.Render("ON")
		}
		rows = append(rows, [2]string{"Kill switch", ks})
		for _, r := range rows {
			b.WriteString(fmt.Sprintf("    %s %s\n", styleLabel.Render(fmt.Sprintf("%-12s", r[0])), styleValue.Render(r[1])))
		}
		// Leak check summary.
		if m.lastLeak != nil {
			b.WriteString("\n    " + styleLabel.Render("Checks      ") + leakSummary(m.lastLeak, m.postIP, m.preIP) + "\n")
		}
	}

	if m.connState == StateIdle {
		p := m.lastProfile()
		name := "none"
		if p != nil {
			name = p.Name
		}
		b.WriteString(styleLabel.Render("    press c to connect with profile ") + styleValue.Render(name) +
			styleLabel.Render(", or 2 to pick another\n"))
	}
	if m.statsErr != "" {
		b.WriteString("\n" + styleError.Render("    stats: "+m.statsErr) + "\n")
	}
	return b.String()
}

func leakSummary(lc *monitor.LeakCheck, postIP, preIP string) string {
	pf := func(ok bool, label string) string {
		if ok {
			return styleOK.Render("✓" + label)
		}
		return styleFail.Render("✗" + label)
	}
	out := pf(lc.DNSOK, "dns") + " " + pf(lc.ResolverOK, "resolver")
	if postIP != "" {
		if preIP != "" && preIP == postIP {
			out += " " + styleFail.Render("✗ip:"+postIP)
		} else {
			out += " " + styleOK.Render("✓ip:"+postIP)
		}
	}
	return out
}

func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w < 1 {
		return ""
	}
	return s[:w-1] + "…"
}

func truncateLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, "…")
	}
	return strings.Join(lines, "\n")
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func (m *Model) viewHelp() string {
	help := `wgo — WireGuard TUI manager

  c        connect (last-used profile) / disconnect
  1–5      switch tabs (also tab / shift+tab)
  k        toggle kill switch for the current session
  t        run DNS leak / connectivity test
  d        disconnect and quit (tears down tunnel + kill switch)
  q        quit (confirmation required if kill switch is on)
  r / x    retry / tear down after a failed connect
  ?        close this help

Profiles tab:
  enter    connect selected profile
  i        import a .conf by path
  e        edit in $EDITOR (backs up first)
  w/l/a    toggle kill switch / allow-LAN / auto-reconnect per profile

Note: wgo has no background daemon. Auto-reconnect and the kill
switch are only managed while wgo is running.`
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		styleHelp.Render(help))
}
