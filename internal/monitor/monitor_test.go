package monitor

import (
	"testing"
	"time"
)

func TestIsStale(t *testing.T) {
	now := time.Now()
	th := 180 * time.Second
	up := now.Add(-10 * time.Minute)

	if IsStale(now.Add(-60*time.Second), up, now, th) {
		t.Error("fresh handshake must not be stale")
	}
	if !IsStale(now.Add(-200*time.Second), up, now, th) {
		t.Error("old handshake must be stale")
	}
	// Zero handshake: stale only once uptime exceeds threshold.
	if IsStale(time.Time{}, now.Add(-30*time.Second), now, th) {
		t.Error("zero handshake on young interface must not be stale")
	}
	if !IsStale(time.Time{}, now.Add(-5*time.Minute), now, th) {
		t.Error("zero handshake on old interface must be stale")
	}
	if IsStale(time.Time{}, time.Time{}, now, th) {
		t.Error("unknown uptime must not be stale")
	}
	if IsStale(now.Add(-time.Hour), up, now, 0) {
		t.Error("zero threshold disables staleness")
	}
}

func TestBackoffDelay(t *testing.T) {
	cases := map[int]time.Duration{
		0: 2 * time.Second,
		1: 2 * time.Second,
		2: 4 * time.Second,
		3: 8 * time.Second,
		4: 16 * time.Second,
		9: 2 * time.Minute, // capped
	}
	for attempt, want := range cases {
		if got := BackoffDelay(attempt); got != want {
			t.Errorf("BackoffDelay(%d) = %v, want %v", attempt, got, want)
		}
	}
}

func TestRateTracker(t *testing.T) {
	rt := NewRateTracker(time.Minute)
	t0 := time.Now()
	rt.Add(t0, 0, 0)
	rt.Add(t0.Add(time.Second), 1000, 500)
	rx, tx := rt.Rates()
	if rx != 1000 || tx != 500 {
		t.Errorf("rates: %v %v", rx, tx)
	}
	// Counter reset (interface bounce) must not produce negative rates.
	rt.Add(t0.Add(2*time.Second), 100, 50)
	rx, tx = rt.Rates()
	if rx != 0 || tx != 0 {
		t.Errorf("negative rates must clamp to 0: %v %v", rx, tx)
	}
	peakRx, _ := rt.Peaks()
	if peakRx != 1000 {
		t.Errorf("peak: %v", peakRx)
	}
	rxs, txs := rt.RateSeries()
	if len(rxs) != 2 || len(txs) != 2 {
		t.Errorf("series lengths: %d %d", len(rxs), len(txs))
	}
}

func TestRateTrackerWindow(t *testing.T) {
	rt := NewRateTracker(10 * time.Second)
	t0 := time.Now()
	rt.Add(t0, 0, 0)
	rt.Add(t0.Add(30*time.Second), 100, 100) // first sample now out of window
	if n := len(rt.samples); n != 1 {
		t.Errorf("expected old samples pruned, have %d", n)
	}
}
