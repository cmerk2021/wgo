// Package verify implements the staged connection verification engine.
package verify

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cmerk2021/wgo/internal/execx"
	"github.com/cmerk2021/wgo/internal/wg"
)

// Stage identifies one verification stage.
type Stage int

const (
	StageInterface Stage = iota // interface exists after wg-quick up
	StageHandshake              // recent, non-zero handshake
	StageData                   // rx bytes increasing
	StageDNS                    // DNS + connectivity probe
)

// Stages in execution order.
var Stages = []Stage{StageInterface, StageHandshake, StageData, StageDNS}

// String returns a human-readable stage name.
func (s Stage) String() string {
	switch s {
	case StageInterface:
		return "Interface up"
	case StageHandshake:
		return "Handshake"
	case StageData:
		return "Data flowing"
	case StageDNS:
		return "DNS / internet"
	}
	return "Unknown"
}

// Result is the outcome of one stage.
type Result struct {
	Stage  Stage
	OK     bool
	Detail string
	Err    error
}

// Options configures the engine.
type Options struct {
	Iface        string
	Timeout      time.Duration // per stage
	DNSCheckHost string
	ProbeTarget  string
	BaselineRx   int64 // rx byte count captured at bring-up
}

// Engine runs the verification stages. All dependencies are injectable so
// the logic can be unit tested with mocks.
type Engine struct {
	Opts Options

	IfaceExists func(name string) bool
	Stats       func(ctx context.Context, iface string) (*wg.DeviceStats, error)
	LookupHost  func(ctx context.Context, host string) ([]string, error)
	Probe       func(ctx context.Context, target string) error
	Now         func() time.Time
	PollEvery   time.Duration
}

// New returns an Engine with real dependencies.
func New(opts Options, client *wg.Client, runner execx.Runner) *Engine {
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}
	return &Engine{
		Opts:        opts,
		IfaceExists: wg.InterfaceExists,
		Stats:       client.Stats,
		LookupHost: func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		},
		Probe:     func(ctx context.Context, target string) error { return probe(ctx, runner, target) },
		Now:       time.Now,
		PollEvery: 500 * time.Millisecond,
	}
}

// probe pings target, falling back to a TCP dial on :443.
func probe(ctx context.Context, runner execx.Runner, target string) error {
	if _, err := runner.LookPath("ping"); err == nil {
		res, err := runner.Run(ctx, "ping", "-c", "1", "-W", "3", target)
		if err == nil && res.ExitCode == 0 {
			return nil
		}
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(target, "443"))
	if err != nil {
		return fmt.Errorf("probe %s unreachable: %w", target, err)
	}
	conn.Close()
	return nil
}

// RunStage executes a single stage, polling until success or timeout.
func (e *Engine) RunStage(ctx context.Context, s Stage) Result {
	deadline := e.Now().Add(e.Opts.Timeout)
	var lastErr error
	for {
		res, done := e.attempt(ctx, s)
		if done {
			return res
		}
		lastErr = res.Err
		if e.Now().After(deadline) {
			detail := fmt.Sprintf("timed out after %s", e.Opts.Timeout)
			return Result{Stage: s, OK: false, Detail: detail, Err: lastErr}
		}
		select {
		case <-ctx.Done():
			return Result{Stage: s, OK: false, Detail: "cancelled", Err: ctx.Err()}
		case <-time.After(e.PollEvery):
		}
	}
}

// attempt runs one check; done is true when the result is final (success).
func (e *Engine) attempt(ctx context.Context, s Stage) (Result, bool) {
	iface := e.Opts.Iface
	switch s {
	case StageInterface:
		if e.IfaceExists(iface) {
			return Result{Stage: s, OK: true, Detail: iface + " exists"}, true
		}
		return Result{Stage: s, Err: fmt.Errorf("interface %s not found", iface)}, false

	case StageHandshake:
		ds, err := e.Stats(ctx, iface)
		if err != nil {
			return Result{Stage: s, Err: err}, false
		}
		hs := ds.LatestHandshake()
		if !hs.IsZero() && e.Now().Sub(hs) < 3*time.Minute {
			return Result{Stage: s, OK: true, Detail: "handshake " + e.Now().Sub(hs).Round(time.Second).String() + " ago"}, true
		}
		return Result{Stage: s, Err: fmt.Errorf("no recent handshake")}, false

	case StageData:
		ds, err := e.Stats(ctx, iface)
		if err != nil {
			return Result{Stage: s, Err: err}, false
		}
		if ds.TotalRx() > e.Opts.BaselineRx {
			return Result{Stage: s, OK: true, Detail: fmt.Sprintf("rx %d bytes", ds.TotalRx())}, true
		}
		return Result{Stage: s, Err: fmt.Errorf("no data received yet")}, false

	case StageDNS:
		if _, err := e.LookupHost(ctx, e.Opts.DNSCheckHost); err != nil {
			return Result{Stage: s, Err: fmt.Errorf("DNS lookup %s: %w", e.Opts.DNSCheckHost, err)}, false
		}
		if err := e.Probe(ctx, e.Opts.ProbeTarget); err != nil {
			return Result{Stage: s, Err: err}, false
		}
		return Result{Stage: s, OK: true, Detail: "DNS + probe OK"}, true
	}
	return Result{Stage: s, Err: fmt.Errorf("unknown stage")}, true
}
