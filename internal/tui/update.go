package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmerk2021/wgo/internal/monitor"
	"github.com/cmerk2021/wgo/internal/profile"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.logView.Width = msg.Width - 4
		m.logView.Height = msg.Height - 8
		if !m.logReady {
			m.logReady = true
		}
		m.logView.SetContent(m.renderLogs())
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		cmds := []tea.Cmd{m.tick()}
		if c := m.pollStats(); c != nil {
			cmds = append(cmds, c)
		}
		if c := m.checkStaleness(); c != nil {
			cmds = append(cmds, c)
		}
		return m, tea.Batch(cmds...)

	case statsMsg:
		m.stats = msg.ds
		m.statsErr = ""
		if m.connState == StateConnected || m.connState == StateStale {
			m.rates.Add(time.Now(), msg.ds.TotalRx(), msg.ds.TotalTx())
		}
		return m, nil

	case statsErrMsg:
		m.statsErr = msg.err.Error()
		return m, nil

	case ctrlEventMsg:
		m.addLog(msg.Message)
		return m, m.waitEvent()

	case ctrlStageMsg:
		for i := range m.stages {
			if m.stages[i].stage == msg.Stage {
				if msg.Running {
					m.stages[i].running = true
				}
				if msg.Result != nil {
					m.stages[i].running = false
					m.stages[i].done = true
					m.stages[i].ok = msg.Result.OK
					m.stages[i].detail = msg.Result.Detail
					if msg.Result.Err != nil && !msg.Result.OK {
						m.stages[i].detail = strings.TrimSpace(m.stages[i].detail + " " + msg.Result.Err.Error())
					}
				}
			}
		}
		return m, m.waitEvent()

	case connectDoneMsg:
		if msg.err != nil {
			m.connState = StateFailed
			if se, ok := msg.err.(*monitor.StageError); ok {
				m.failOutput = se.Output
			}
			m.addLog("connect failed: " + msg.err.Error())
			return m, nil
		}
		if m.connState == StateReconnecting {
			m.addLog("reconnect successful")
		}
		m.connState = StateConnected
		m.connectedAt = time.Now()
		m.reconnAttempts = 0
		m.rates.Reset()
		m.addLog("connected: " + m.activeIface)
		return m, m.runLeakTest()

	case disconnDoneMsg:
		m.connState = StateIdle
		m.activeIface = ""
		m.stats = nil
		m.rates.Reset()
		m.lastLeak = nil
		m.postIP = ""
		if msg.err != nil {
			m.addLog("disconnect: " + msg.err.Error())
		} else {
			m.addLog("disconnected")
		}
		return m, nil

	case teardownQuitMsg:
		return m, tea.Quit

	case leakMsg:
		lc := monitor.LeakCheck(msg)
		m.lastLeak = &lc
		m.addLog(fmt.Sprintf("leak check: dns=%v (%s) resolver=%v (%s)",
			lc.DNSOK, lc.DNSDetail, lc.ResolverOK, lc.ResolverDetail))
		return m, nil

	case publicIPMsg:
		if msg.errStr != "" {
			m.addLog("public IP lookup failed (informational): " + msg.errStr)
		} else {
			m.postIP = msg.ip
			note := ""
			if m.preIP != "" && m.preIP == msg.ip {
				note = " (WARNING: same as pre-connect IP)"
			}
			m.addLog("public IP: " + msg.ip + note)
		}
		return m, nil

	case ksToggleMsg:
		if msg.err != nil {
			m.flash = "kill switch: " + msg.err.Error()
			m.addLog("kill switch toggle failed: " + msg.err.Error())
		} else if msg.on {
			m.addLog("kill switch enabled for this session")
		} else {
			m.addLog("kill switch disabled for this session")
		}
		return m, nil

	case editorDoneMsg:
		m.reloadProfiles()
		if msg.err != nil {
			m.flash = "edit: " + msg.err.Error()
			m.addLog("edit " + msg.name + " failed validation: " + msg.err.Error())
		} else {
			m.addLog("edited profile " + msg.name + " (backup kept at " + msg.name + ".conf.bak)")
		}
		return m, nil

	case importDoneMsg:
		m.reloadProfiles()
		if msg.err != nil {
			if strings.Contains(msg.err.Error(), "confirm overwrite") {
				m.confirmOver = msg.name
				return m, nil
			}
			m.flash = "import: " + msg.err.Error()
		} else {
			m.addLog("imported profile from " + msg.name)
		}
		return m, nil

	case reconnectTickMsg:
		return m, m.doReconnect(msg.attempt)

	case preIPMsg:
		if msg.ip != "" {
			m.preIP = msg.ip
			m.addLog("pre-connect public IP: " + msg.ip)
		}
		return m, nil

	default:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	m.flash = ""

	// Modal: quit confirmation.
	if m.confirmQuit {
		m.confirmQuit = false
		if ConfirmQuit(key) {
			ctrl := m.ctrl
			return m, func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err := ctrl.DisableKillswitch(ctx)
				return teardownQuitMsg{err}
			}
		}
		return m, nil
	}

	// Modal: import overwrite confirmation.
	if m.confirmOver != "" {
		src := m.confirmOver
		m.confirmOver = ""
		if key == "y" || key == "Y" {
			return m, func() tea.Msg {
				_, err := profile.Import(src, true)
				return importDoneMsg{name: src, err: err}
			}
		}
		return m, nil
	}

	// Modal: import path input.
	if m.importing {
		switch key {
		case "enter":
			src := strings.TrimSpace(m.importInput.Value())
			m.importing = false
			m.importInput.Blur()
			if src == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				_, err := profile.Import(src, false)
				return importDoneMsg{name: src, err: err}
			}
		case "esc":
			m.importing = false
			m.importInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.importInput, cmd = m.importInput.Update(msg)
		return m, cmd
	}

	// Modal: settings value editing.
	if m.settingsEdit {
		switch key {
		case "enter":
			m.applySettingEdit()
			m.settingsEdit = false
			m.settingsInput.Blur()
			return m, nil
		case "esc":
			m.settingsEdit = false
			m.settingsInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.settingsInput, cmd = m.settingsInput.Update(msg)
		return m, cmd
	}

	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	switch key {
	case "ctrl+c":
		return m, m.quitRequested()
	case "q":
		return m, m.quitRequested()
	case "d":
		// Disconnect and quit in one step.
		return m, m.startDisconnect(true)
	case "?":
		m.showHelp = true
		return m, nil
	case "1", "2", "3", "4", "5":
		m.tab = Tab(int(key[0] - '1'))
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + tabCount - 1) % tabCount
		return m, nil
	case "c":
		return m, m.connectToggle()
	case "k":
		return m, m.toggleKillswitch()
	case "t":
		if m.connState == StateConnected || m.connState == StateStale {
			m.addLog("running on-demand leak test")
			return m, m.runLeakTest()
		}
		return m, nil
	case "r":
		if m.connState == StateFailed && m.activeProf != nil {
			m.addLog("retrying connection")
			return m, m.startConnect(m.activeProf)
		}
	case "x":
		if m.connState == StateFailed {
			m.addLog("tearing down after failed connect")
			return m, m.startDisconnect(false)
		}
	}

	switch m.tab {
	case TabProfiles:
		return m.handleProfilesKey(key)
	case TabSettings:
		return m.handleSettingsKey(key)
	case TabLogs:
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) quitRequested() tea.Cmd {
	if QuitDecision(m.ctrl.KillswitchActive()) == QuitNeedsConfirm {
		m.confirmQuit = true
		return nil
	}
	return tea.Quit
}

func (m *Model) connectToggle() tea.Cmd {
	switch m.connState {
	case StateIdle, StateFailed:
		return m.startConnect(m.lastProfile())
	case StateConnected, StateStale:
		return m.startDisconnect(false)
	}
	return nil
}

func (m *Model) toggleKillswitch() tea.Cmd {
	ctrl := m.ctrl
	prof := m.activeProf
	if prof == nil {
		prof = m.lastProfile()
	}
	if prof == nil {
		m.flash = "no profile to derive kill switch rules from"
		return nil
	}
	if ctrl.KillswitchActive() {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return ksToggleMsg{on: false, err: ctrl.DisableKillswitch(ctx)}
		}
	}
	p := *prof
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return ksToggleMsg{on: true, err: ctrl.EnableKillswitch(ctx, &p)}
	}
}

// checkStaleness monitors handshake age and drives auto-reconnect.
func (m *Model) checkStaleness() tea.Cmd {
	if m.connState != StateConnected && m.connState != StateStale {
		return nil
	}
	if m.stats == nil {
		return nil
	}
	stale := monitor.IsStale(m.stats.LatestHandshake(), m.connectedAt, time.Now(), m.staleThreshold())
	if !stale {
		if m.connState == StateStale {
			m.connState = StateConnected
			m.addLog("handshake recovered; tunnel healthy")
		}
		return nil
	}
	if m.connState == StateConnected {
		m.connState = StateStale
		m.addLog(fmt.Sprintf("handshake stale (> %s)", m.staleThreshold()))
		if m.effective().AutoReconnect && m.activeProf != nil {
			m.connState = StateReconnecting
			m.reconnAttempts = 1
			m.addLog("auto-reconnect: attempt 1")
			return m.doReconnect(1)
		}
	}
	return nil
}

// doReconnect runs one recovery attempt: down, then re-connect.
func (m *Model) doReconnect(attempt int) tea.Cmd {
	if attempt > m.cfg.Global.ReconnectMaxAttempts {
		m.connState = StateFailed
		m.addLog("auto-reconnect: gave up after max attempts")
		return nil
	}
	m.reconnAttempts = attempt
	ctrl, iface := m.ctrl, m.activeIface
	prof := *m.activeProf
	m.connState = StateReconnecting
	delay := monitor.BackoffDelay(attempt)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = ctrl.Client.Down(ctx, iface)
		time.Sleep(delay)
		err := ctrl.Connect(ctx, prof)
		if err != nil {
			return reconnectTickMsg{attempt: attempt + 1}
		}
		return connectDoneMsg{nil}
	}
}

// editProfile backs up and opens the profile in $EDITOR via tea.ExecProcess.
func (m *Model) editProfile(p profile.Profile) tea.Cmd {
	bak, err := profile.Backup(p.Path)
	if err != nil {
		m.flash = "backup failed: " + err.Error()
		return nil
	}
	m.addLog("backed up " + p.Path + " to " + bak)
	ed, err := profile.EditorCommand(exec.LookPath)
	if err != nil {
		m.flash = err.Error()
		return nil
	}
	cmd := exec.Command(ed, p.Path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	name := p.Name
	path := p.Path
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return editorDoneMsg{name: name, err: err}
		}
		_, perr := profile.ParseFile(path)
		return editorDoneMsg{name: name, err: perr}
	})
}
