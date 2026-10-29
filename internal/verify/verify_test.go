package verify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cmerk2021/wgo/internal/wg"
)

// fakeClock advances by step on every Now() call so timeouts trigger quickly.
type fakeClock struct {
	t    time.Time
	step time.Duration
}

func (f *fakeClock) Now() time.Time {
	f.t = f.t.Add(f.step)
	return f.t
}

func testEngine() (*Engine, *fakeClock) {
	clk := &fakeClock{t: time.Unix(1000000, 0), step: 0}
	e := &Engine{
		Opts:      Options{Iface: "wg0", Timeout: 10 * time.Second, DNSCheckHost: "example.com", ProbeTarget: "1.1.1.1"},
		Now:       clk.Now,
		PollEvery: time.Millisecond,
	}
	return e, clk
}

func TestStageInterface(t *testing.T) {
	e, _ := testEngine()
	e.IfaceExists = func(string) bool { return true }
	res := e.RunStage(context.Background(), StageInterface)
	if !res.OK {
		t.Errorf("expected success: %+v", res)
	}

	e2, clk := testEngine()
	clk.step = 3 * time.Second // exceed timeout quickly
	e2.IfaceExists = func(string) bool { return false }
	res = e2.RunStage(context.Background(), StageInterface)
	if res.OK {
		t.Error("expected timeout failure")
	}
}

func statsFn(hs time.Time, rx int64) func(context.Context, string) (*wg.DeviceStats, error) {
	return func(context.Context, string) (*wg.DeviceStats, error) {
		return &wg.DeviceStats{Name: "wg0", Peers: []wg.PeerStats{{LastHandshake: hs, RxBytes: rx}}}, nil
	}
}

func TestStageHandshake(t *testing.T) {
	e, clk := testEngine()
	e.Stats = statsFn(clk.t.Add(-30*time.Second), 0)
	res := e.RunStage(context.Background(), StageHandshake)
	if !res.OK {
		t.Errorf("recent handshake should pass: %+v", res)
	}

	// Zero handshake fails until timeout.
	e2, clk2 := testEngine()
	clk2.step = 3 * time.Second
	e2.Stats = statsFn(time.Time{}, 0)
	res = e2.RunStage(context.Background(), StageHandshake)
	if res.OK {
		t.Error("zero handshake must not pass")
	}

	// Old handshake fails.
	e3, clk3 := testEngine()
	clk3.step = 3 * time.Second
	e3.Stats = statsFn(clk3.t.Add(-10*time.Minute), 0)
	res = e3.RunStage(context.Background(), StageHandshake)
	if res.OK {
		t.Error("stale handshake must not pass")
	}
}

func TestStageData(t *testing.T) {
	e, _ := testEngine()
	e.Opts.BaselineRx = 100
	e.Stats = statsFn(time.Time{}, 500)
	res := e.RunStage(context.Background(), StageData)
	if !res.OK {
		t.Errorf("rx above baseline should pass: %+v", res)
	}

	e2, clk := testEngine()
	clk.step = 3 * time.Second
	e2.Opts.BaselineRx = 500
	e2.Stats = statsFn(time.Time{}, 500)
	res = e2.RunStage(context.Background(), StageData)
	if res.OK {
		t.Error("no rx increase must not pass")
	}
}

func TestStageDNS(t *testing.T) {
	e, _ := testEngine()
	e.LookupHost = func(context.Context, string) ([]string, error) { return []string{"93.184.216.34"}, nil }
	e.Probe = func(context.Context, string) error { return nil }
	res := e.RunStage(context.Background(), StageDNS)
	if !res.OK {
		t.Errorf("dns+probe ok should pass: %+v", res)
	}

	e2, clk := testEngine()
	clk.step = 3 * time.Second
	e2.LookupHost = func(context.Context, string) ([]string, error) { return nil, errors.New("NXDOMAIN") }
	e2.Probe = func(context.Context, string) error { return nil }
	res = e2.RunStage(context.Background(), StageDNS)
	if res.OK {
		t.Error("dns failure must not pass")
	}

	e3, clk3 := testEngine()
	clk3.step = 3 * time.Second
	e3.LookupHost = func(context.Context, string) ([]string, error) { return []string{"1.2.3.4"}, nil }
	e3.Probe = func(context.Context, string) error { return errors.New("unreachable") }
	res = e3.RunStage(context.Background(), StageDNS)
	if res.OK {
		t.Error("probe failure must not pass")
	}
}

func TestStageRetriesUntilSuccess(t *testing.T) {
	e, _ := testEngine()
	calls := 0
	e.IfaceExists = func(string) bool {
		calls++
		return calls >= 3
	}
	res := e.RunStage(context.Background(), StageInterface)
	if !res.OK || calls < 3 {
		t.Errorf("expected polling until success: ok=%v calls=%d", res.OK, calls)
	}
}

func TestStageNames(t *testing.T) {
	want := []string{"Interface up", "Handshake", "Data flowing", "DNS / internet"}
	for i, s := range Stages {
		if s.String() != want[i] {
			t.Errorf("stage %d name %q, want %q", i, s.String(), want[i])
		}
	}
}
