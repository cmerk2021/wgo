// Package tui implements the Bubble Tea terminal UI.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmerk2021/wgo/internal/config"
	"github.com/cmerk2021/wgo/internal/monitor"
	"github.com/cmerk2021/wgo/internal/profile"
	"github.com/cmerk2021/wgo/internal/verify"
	"github.com/cmerk2021/wgo/internal/wg"
)

// Tab identifies a UI tab.
type Tab int

const (
	TabDashboard Tab = iota
	TabProfiles
	TabThroughput
	TabLogs
	TabSettings
	tabCount
)

var tabNames = []string{"Dashboard", "Profiles", "Throughput", "Logs", "Settings"}

// ConnState is the connection lifecycle state.
type ConnState int

const (
	StateIdle ConnState = iota
	StateConnecting
	StateConnected
	StateStale
	StateReconnecting
	StateFailed
	StateDisconnecting
)

type stageStatus struct {
	stage   verify.Stage
	running bool
	done    bool
	ok      bool
	detail  string
}

// Messages.
type (
	tickMsg          time.Time
	statsMsg         struct{ ds *wg.DeviceStats }
	statsErrMsg      struct{ err error }
	ctrlEventMsg     monitor.Event
	ctrlStageMsg     monitor.StageEvent
	connectDoneMsg   struct{ err error }
	disconnDoneMsg   struct{ err error }
	teardownQuitMsg  struct{ err error }
	leakMsg          monitor.LeakCheck
	publicIPMsg      struct{ ip, errStr string }
	ksToggleMsg      struct{ on bool; err error }
	editorDoneMsg    struct{ name string; err error }
	importDoneMsg    struct{ name string; err error }
	reconnectTickMsg struct{ attempt int }
	preIPMsg         struct{ ip string }
)

type logEntry struct {
	at   time.Time
	text string
}

const maxLogLines = 500

// Model is the root Bubble Tea model.
type Model struct {
	cfg    *config.Config
	state  *config.State
	ctrl   *monitor.Controller
	client *wg.Client

	width, height int
	tab           Tab

	connState   ConnState
	activeIface string
	activeProf  *profile.Profile
	connectedAt time.Time
	stages      []stageStatus
	failOutput  string

	stats     *wg.DeviceStats
	statsErr  string
	rates     *monitor.RateTracker
	lastLeak  *monitor.LeakCheck
	preIP     string
	postIP    string

	profiles    []profile.Profile
	profCursor  int
	importInput textinput.Model
	importing   bool

	settingsCursor int
	settingsInput  textinput.Model
	settingsEdit   bool

	logs     []logEntry
	logView  viewport.Model
	logReady bool

	spin         spinner.Model
	showHelp     bool
	confirmQuit  bool
	confirmOver  string // pending overwrite path for import
	flash        string

	reconnAttempts int

	events chan tea.Msg

	ctx    context.Context
	cancel context.CancelFunc
}

// New builds the root model.
func New(cfg *config.Config, st *config.State, ctrl *monitor.Controller, client *wg.Client) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	ti := textinput.New()
	ti.Placeholder = "/path/to/profile.conf"
	si := textinput.New()
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		cfg:         cfg,
		state:       st,
		ctrl:        ctrl,
		client:      client,
		spin:        sp,
		importInput: ti,
		settingsInput: si,
		rates:       monitor.NewRateTracker(120 * time.Second),
		events:      make(chan tea.Msg, 256),
		ctx:         ctx,
		cancel:      cancel,
	}
	ctrl.OnEvent = func(e monitor.Event) {
		select {
		case m.events <- ctrlEventMsg(e):
		default:
		}
	}
	ctrl.OnStage = func(e monitor.StageEvent) {
		select {
		case m.events <- ctrlStageMsg(e):
		default:
		}
	}
	m.reloadProfiles()
	return m
}

func (m *Model) reloadProfiles() {
	profs, err := profile.List()
	if err != nil {
		m.addLog("profiles: " + err.Error())
	}
	m.profiles = profs
	if m.profCursor >= len(profs) {
		m.profCursor = 0
	}
}

func (m *Model) addLog(text string) {
	m.logs = append(m.logs, logEntry{at: time.Now(), text: text})
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}
	if m.logReady {
		m.logView.SetContent(m.renderLogs())
		m.logView.GotoBottom()
	}
}

// DetectExisting marks already-up WireGuard interfaces as connected.
func (m *Model) DetectExisting() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ifaces, err := m.client.ActiveInterfaces(ctx)
	if err != nil || len(ifaces) == 0 {
		return
	}
	name := ifaces[0]
	m.activeIface = name
	m.connState = StateConnected
	m.connectedAt = time.Now() // best effort; actual uptime unknown
	for i := range m.profiles {
		if m.profiles[i].Name == name {
			m.activeProf = &m.profiles[i]
		}
	}
	m.addLog(fmt.Sprintf("detected already-running interface %s (started outside wgo)", name))
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.tick(), m.waitEvent()}
	if m.connState == StateIdle {
		url := m.cfg.Global.PublicIPURL
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ip, err := monitor.PublicIP(ctx, url)
			if err != nil {
				return preIPMsg{""}
			}
			return preIPMsg{ip}
		})
	}
	return tea.Batch(cmds...)
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) waitEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func (m *Model) pollStats() tea.Cmd {
	iface := m.activeIface
	if iface == "" {
		return nil
	}
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ds, err := client.Stats(ctx, iface)
		if err != nil {
			return statsErrMsg{err}
		}
		return statsMsg{ds}
	}
}

func (m *Model) lastProfile() *profile.Profile {
	name := m.state.LastProfile
	if name == "" && len(m.profiles) > 0 {
		return &m.profiles[0]
	}
	for i := range m.profiles {
		if m.profiles[i].Name == name {
			return &m.profiles[i]
		}
	}
	if len(m.profiles) > 0 {
		return &m.profiles[0]
	}
	return nil
}

func (m *Model) startConnect(p *profile.Profile) tea.Cmd {
	if p == nil {
		m.flash = "no profiles found in /etc/wireguard"
		return nil
	}
	if p.ParseErr != nil {
		m.flash = fmt.Sprintf("profile %s is invalid: %v", p.Name, p.ParseErr)
		return nil
	}
	m.connState = StateConnecting
	m.activeProf = p
	m.activeIface = p.Name
	m.failOutput = ""
	m.stages = nil
	for _, s := range verify.Stages {
		m.stages = append(m.stages, stageStatus{stage: s})
	}
	m.state.LastProfile = p.Name
	if err := m.state.Save(); err != nil {
		m.addLog("state save: " + err.Error())
	}
	ctrl, prof, ctx := m.ctrl, *p, m.ctx
	return func() tea.Msg {
		err := ctrl.Connect(ctx, prof)
		return connectDoneMsg{err}
	}
}

func (m *Model) startDisconnect(quitAfter bool) tea.Cmd {
	iface := m.activeIface
	if iface == "" {
		if quitAfter {
			return tea.Quit
		}
		return nil
	}
	m.connState = StateDisconnecting
	ctrl := m.ctrl
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := ctrl.Disconnect(ctx, iface)
		if quitAfter {
			return teardownQuitMsg{err}
		}
		return disconnDoneMsg{err}
	}
}

func (m *Model) runLeakTest() tea.Cmd {
	if m.activeProf == nil {
		return nil
	}
	eff := m.cfg.Resolve(m.activeProf.Name)
	expected := m.activeProf.DNS
	url := m.cfg.Global.PublicIPURL
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			return leakMsg(monitor.CheckDNS(ctx, eff.DNSCheckHost, expected, wg.ResolvConfDNS()))
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ip, err := monitor.PublicIP(ctx, url)
			if err != nil {
				return publicIPMsg{errStr: err.Error()}
			}
			return publicIPMsg{ip: ip}
		},
	)
}

// Shutdown cancels background work (called after the program exits).
func (m *Model) Shutdown() { m.cancel() }

func (m *Model) effective() config.Effective {
	name := ""
	if m.activeProf != nil {
		name = m.activeProf.Name
	}
	return m.cfg.Resolve(name)
}

func (m *Model) stageTimeout() time.Duration {
	return time.Duration(m.cfg.Global.StageTimeoutSeconds) * time.Second
}

func (m *Model) staleThreshold() time.Duration {
	return time.Duration(m.cfg.Global.HandshakeStaleSeconds) * time.Second
}

func (m *Model) renderLogs() string {
	var b strings.Builder
	for _, e := range m.logs {
		b.WriteString(styleLabel.Render(e.at.Format("15:04:05")) + " " + e.text + "\n")
	}
	return b.String()
}
