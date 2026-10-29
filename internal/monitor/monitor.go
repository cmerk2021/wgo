// Package monitor provides stats tracking, staleness detection, reconnect
// backoff, and DNS/public-IP checks.
package monitor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Sample is one throughput measurement.
type Sample struct {
	At time.Time
	Rx int64 // cumulative bytes
	Tx int64
}

// RateTracker keeps a rolling window of throughput samples.
type RateTracker struct {
	window  time.Duration
	samples []Sample
	peakRx  float64
	peakTx  float64
}

// NewRateTracker returns a tracker keeping samples for window.
func NewRateTracker(window time.Duration) *RateTracker {
	return &RateTracker{window: window}
}

// Add records a cumulative byte sample.
func (r *RateTracker) Add(at time.Time, rx, tx int64) {
	r.samples = append(r.samples, Sample{At: at, Rx: rx, Tx: tx})
	cut := at.Add(-r.window)
	i := 0
	for i < len(r.samples) && r.samples[i].At.Before(cut) {
		i++
	}
	r.samples = r.samples[i:]
	rxRate, txRate := r.Rates()
	if rxRate > r.peakRx {
		r.peakRx = rxRate
	}
	if txRate > r.peakTx {
		r.peakTx = txRate
	}
}

// Rates returns the current rx/tx rates in bytes per second based on the
// two most recent samples.
func (r *RateTracker) Rates() (rx, tx float64) {
	n := len(r.samples)
	if n < 2 {
		return 0, 0
	}
	a, b := r.samples[n-2], r.samples[n-1]
	dt := b.At.Sub(a.At).Seconds()
	if dt <= 0 {
		return 0, 0
	}
	rx = float64(b.Rx-a.Rx) / dt
	tx = float64(b.Tx-a.Tx) / dt
	if rx < 0 {
		rx = 0
	}
	if tx < 0 {
		tx = 0
	}
	return rx, tx
}

// Peaks returns peak rx/tx rates seen this session (bytes/s).
func (r *RateTracker) Peaks() (rx, tx float64) { return r.peakRx, r.peakTx }

// RateSeries returns per-interval rates (bytes/s) for sparkline rendering.
func (r *RateTracker) RateSeries() (rx, tx []float64) {
	for i := 1; i < len(r.samples); i++ {
		a, b := r.samples[i-1], r.samples[i]
		dt := b.At.Sub(a.At).Seconds()
		if dt <= 0 {
			continue
		}
		rv := float64(b.Rx-a.Rx) / dt
		tv := float64(b.Tx-a.Tx) / dt
		if rv < 0 {
			rv = 0
		}
		if tv < 0 {
			tv = 0
		}
		rx = append(rx, rv)
		tx = append(tx, tv)
	}
	return
}

// Reset clears samples and peaks.
func (r *RateTracker) Reset() {
	r.samples = nil
	r.peakRx, r.peakTx = 0, 0
}

// IsStale reports whether a handshake timestamp exceeds threshold.
// A zero handshake on an up interface is treated as stale only after
// upSince itself exceeds the threshold.
func IsStale(lastHandshake, upSince, now time.Time, threshold time.Duration) bool {
	if threshold <= 0 {
		return false
	}
	if lastHandshake.IsZero() {
		return !upSince.IsZero() && now.Sub(upSince) > threshold
	}
	return now.Sub(lastHandshake) > threshold
}

// BackoffDelay returns the exponential backoff delay for attempt (1-based),
// capped at 2 minutes. Attempt 1 => 2s, 2 => 4s, 3 => 8s, ...
func BackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := 2 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= 2*time.Minute {
			return 2 * time.Minute
		}
	}
	return d
}

// PublicIP fetches the caller's public IP via url. Informational only;
// failures must never fail a connection.
func PublicIP(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("unexpected response from %s", url)
	}
	return ip, nil
}

// LeakCheck is the result of a DNS/connectivity spot check.
type LeakCheck struct {
	DNSOK          bool
	DNSDetail      string
	ResolverOK     bool // expected resolver in use (best-effort)
	ResolverDetail string
	PublicIP       string
	PublicIPErr    string
}

// CheckDNS runs a DNS resolution test and compares /etc/resolv.conf
// nameservers against the profile's expected DNS servers.
func CheckDNS(ctx context.Context, host string, expectedDNS, systemDNS []string) LeakCheck {
	lc := LeakCheck{}
	resolver := net.DefaultResolver
	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil {
		lc.DNSDetail = err.Error()
	} else {
		lc.DNSOK = true
		lc.DNSDetail = fmt.Sprintf("%s -> %s", host, strings.Join(addrs, ", "))
	}
	if len(expectedDNS) == 0 {
		lc.ResolverOK = true
		lc.ResolverDetail = "no DNS= in profile (not checked)"
		return lc
	}
	for _, sys := range systemDNS {
		for _, exp := range expectedDNS {
			if sys == exp {
				lc.ResolverOK = true
				lc.ResolverDetail = "resolver " + sys + " matches profile"
				return lc
			}
		}
	}
	lc.ResolverDetail = fmt.Sprintf("system resolvers %v do not include profile DNS %v",
		systemDNS, expectedDNS)
	return lc
}
