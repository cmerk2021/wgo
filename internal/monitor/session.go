package monitor

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cmerk2021/wgo/internal/config"
	"github.com/cmerk2021/wgo/internal/execx"
	"github.com/cmerk2021/wgo/internal/killswitch"
	"github.com/cmerk2021/wgo/internal/profile"
	"github.com/cmerk2021/wgo/internal/verify"
	"github.com/cmerk2021/wgo/internal/wg"
)

// Event is a progress/log event emitted by the Controller.
type Event struct {
	At      time.Time
	Message string
}

// StageEvent reports a verification stage transition.
type StageEvent struct {
	Stage   verify.Stage
	Running bool
	Result  *verify.Result
}

// Controller orchestrates connect/disconnect and the kill switch. It is
// shared by the TUI and the CLI.
type Controller struct {
	Cfg    *config.Config
	Client *wg.Client
	Runner execx.Runner

	OnEvent func(Event)      // optional log sink
	OnStage func(StageEvent) // optional stage progress sink

	mu        sync.Mutex
	ksBackend killswitch.Backend
	ksOn      bool
}

// NewController builds a Controller.
func NewController(cfg *config.Config, client *wg.Client, runner execx.Runner) *Controller {
	return &Controller{Cfg: cfg, Client: client, Runner: runner}
}

func (c *Controller) log(format string, a ...any) {
	if c.OnEvent != nil {
		c.OnEvent(Event{At: time.Now(), Message: fmt.Sprintf(format, a...)})
	}
}

func (c *Controller) stage(ev StageEvent) {
	if c.OnStage != nil {
		c.OnStage(ev)
	}
}

// StageError describes a failed verification stage with captured output.
type StageError struct {
	Stage  verify.Stage
	Detail string
	Output string // captured wg-quick output when relevant
	Err    error
}

func (e *StageError) Error() string {
	msg := fmt.Sprintf("stage %q failed: %s", e.Stage, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Connect brings up prof with staged verification. It returns a *StageError
// on verification failure. The interface is left up for retry/teardown.
func (c *Controller) Connect(ctx context.Context, prof profile.Profile) error {
	eff := c.Cfg.Resolve(prof.Name)
	iface := prof.Name

	// Pre-resolve endpoint and apply the kill switch before bring-up so the
	// tunnel can connect through it.
	if eff.Killswitch {
		if err := c.applyKillswitch(ctx, &prof, eff); err != nil {
			return fmt.Errorf("kill switch: %w", err)
		}
	}

	baseline := int64(0)
	if wg.InterfaceExists(iface) {
		if ds, err := c.Client.Stats(ctx, iface); err == nil {
			baseline = ds.TotalRx()
		}
	}

	c.log("running wg-quick up %s", iface)
	out, err := c.Client.Up(ctx, iface)
	if out != "" {
		c.log("wg-quick: %s", out)
	}
	if err != nil {
		return &StageError{Stage: verify.StageInterface, Detail: "wg-quick up failed", Output: out, Err: err}
	}

	eng := verify.New(verify.Options{
		Iface:        iface,
		Timeout:      time.Duration(c.Cfg.Global.StageTimeoutSeconds) * time.Second,
		DNSCheckHost: eff.DNSCheckHost,
		ProbeTarget:  eff.ProbeTarget,
		BaselineRx:   baseline,
	}, c.Client, c.Runner)

	for _, s := range verify.Stages {
		c.stage(StageEvent{Stage: s, Running: true})
		res := eng.RunStage(ctx, s)
		c.stage(StageEvent{Stage: s, Result: &res})
		if res.OK {
			c.log("stage %q ok: %s", s, res.Detail)
			continue
		}
		c.log("stage %q FAILED: %s", s, res.Detail)
		return &StageError{Stage: s, Detail: res.Detail, Output: out, Err: res.Err}
	}

	// Refresh kill switch with the live endpoint (handles hostname endpoints
	// and endpoint roaming).
	if eff.Killswitch {
		if err := c.refreshKillswitchFromLive(ctx, &prof, eff); err != nil {
			c.log("kill switch refresh: %v", err)
		}
	}
	return nil
}

// Disconnect tears down iface and removes the kill switch.
func (c *Controller) Disconnect(ctx context.Context, iface string) error {
	c.log("running wg-quick down %s", iface)
	out, err := c.Client.Down(ctx, iface)
	if out != "" {
		c.log("wg-quick: %s", out)
	}
	if ksErr := c.DisableKillswitch(ctx); ksErr != nil {
		c.log("kill switch removal: %v", ksErr)
	}
	return err
}

// KillswitchActive reports whether this controller applied a kill switch.
func (c *Controller) KillswitchActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ksOn
}

// EnableKillswitch applies rules for prof (used by the live toggle).
func (c *Controller) EnableKillswitch(ctx context.Context, prof *profile.Profile) error {
	eff := c.Cfg.Resolve(prof.Name)
	if err := c.refreshKillswitchFromLive(ctx, prof, eff); err == nil {
		return nil
	}
	return c.applyKillswitch(ctx, prof, eff)
}

// DisableKillswitch removes rules. Idempotent.
func (c *Controller) DisableKillswitch(ctx context.Context) error {
	c.mu.Lock()
	backend := c.ksBackend
	wasOn := c.ksOn
	c.ksOn = false
	c.mu.Unlock()
	if backend == nil {
		b, err := killswitch.Detect(c.Runner)
		if err != nil {
			if wasOn {
				return err
			}
			return nil
		}
		backend = b
	}
	if err := backend.Remove(ctx); err != nil {
		return err
	}
	if wasOn {
		c.log("kill switch removed")
	}
	return nil
}

func (c *Controller) backend() (killswitch.Backend, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ksBackend != nil {
		return c.ksBackend, nil
	}
	b, err := killswitch.Detect(c.Runner)
	if err != nil {
		return nil, err
	}
	c.ksBackend = b
	return b, nil
}

func (c *Controller) applyKillswitch(ctx context.Context, prof *profile.Profile, eff config.Effective) error {
	ips, port := c.resolveEndpoint(ctx, prof)
	return c.applyRules(ctx, prof.Name, ips, port, eff)
}

func (c *Controller) refreshKillswitchFromLive(ctx context.Context, prof *profile.Profile, eff config.Effective) error {
	ds, err := c.Client.Stats(ctx, prof.Name)
	if err != nil {
		return err
	}
	ep := ds.Endpoint()
	if ep == "" {
		return fmt.Errorf("no live endpoint")
	}
	host, port, err := net.SplitHostPort(ep)
	if err != nil {
		host, port = ep, ""
	}
	return c.applyRules(ctx, prof.Name, []string{host}, port, eff)
}

func (c *Controller) applyRules(ctx context.Context, iface string, ips []string, port string, eff config.Effective) error {
	backend, err := c.backend()
	if err != nil {
		return err
	}
	rules := killswitch.Rules{
		Iface:        iface,
		EndpointIPs:  ips,
		EndpointPort: port,
		AllowLAN:     eff.AllowLAN,
		LANRanges:    c.Cfg.Global.LANRanges,
		AllowDHCP:    true,
	}
	if err := backend.Apply(ctx, rules); err != nil {
		return err
	}
	c.mu.Lock()
	c.ksOn = true
	c.mu.Unlock()
	c.log("kill switch applied via %s (endpoint %v port %s)", backend.Name(), ips, port)
	return nil
}

// resolveEndpoint returns the profile endpoint IPs (resolving hostnames)
// and port.
func (c *Controller) resolveEndpoint(ctx context.Context, prof *profile.Profile) ([]string, string) {
	host := prof.EndpointHost()
	port := prof.EndpointPort()
	if host == "" {
		return nil, port
	}
	if ip := net.ParseIP(host); ip != nil {
		return []string{host}, port
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		c.log("resolve endpoint %s: %v", host, err)
		return nil, port
	}
	return addrs, port
}
