// Package wg talks to WireGuard via wgctrl with a `wg show dump` fallback,
// and runs wg-quick for interface lifecycle.
package wg

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/cmerk2021/wgo/internal/execx"
)

// PeerStats holds live stats for one peer.
type PeerStats struct {
	PublicKey     string
	Endpoint      string
	LastHandshake time.Time
	RxBytes       int64
	TxBytes       int64
	AllowedIPs    []string
}

// DeviceStats holds live stats for an interface.
type DeviceStats struct {
	Name       string
	ListenPort int
	Peers      []PeerStats
}

// TotalRx sums peer rx bytes.
func (d *DeviceStats) TotalRx() int64 {
	var n int64
	for _, p := range d.Peers {
		n += p.RxBytes
	}
	return n
}

// TotalTx sums peer tx bytes.
func (d *DeviceStats) TotalTx() int64 {
	var n int64
	for _, p := range d.Peers {
		n += p.TxBytes
	}
	return n
}

// LatestHandshake returns the most recent handshake across peers.
func (d *DeviceStats) LatestHandshake() time.Time {
	var t time.Time
	for _, p := range d.Peers {
		if p.LastHandshake.After(t) {
			t = p.LastHandshake
		}
	}
	return t
}

// Endpoint returns the first peer endpoint.
func (d *DeviceStats) Endpoint() string {
	for _, p := range d.Peers {
		if p.Endpoint != "" {
			return p.Endpoint
		}
	}
	return ""
}

// Client provides WireGuard operations.
type Client struct {
	runner execx.Runner
}

// New returns a Client using runner for external commands.
func New(runner execx.Runner) *Client {
	return &Client{runner: runner}
}

// ActiveInterfaces lists WireGuard interfaces that are currently up.
func (c *Client) ActiveInterfaces(ctx context.Context) ([]string, error) {
	if cl, err := wgctrl.New(); err == nil {
		defer cl.Close()
		devs, err := cl.Devices()
		if err == nil {
			names := make([]string, 0, len(devs))
			for _, d := range devs {
				names = append(names, d.Name)
			}
			sort.Strings(names)
			return names, nil
		}
	}
	// Fallback: wg show interfaces
	res, err := c.runner.Run(ctx, "wg", "show", "interfaces")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(res.Stdout)
	sort.Strings(fields)
	return fields, nil
}

// Stats returns live stats for iface via wgctrl, falling back to `wg show dump`.
func (c *Client) Stats(ctx context.Context, iface string) (*DeviceStats, error) {
	if cl, err := wgctrl.New(); err == nil {
		defer cl.Close()
		if dev, err := cl.Device(iface); err == nil {
			ds := &DeviceStats{Name: dev.Name, ListenPort: dev.ListenPort}
			for _, p := range dev.Peers {
				ps := PeerStats{
					PublicKey:     p.PublicKey.String(),
					LastHandshake: p.LastHandshakeTime,
					RxBytes:       p.ReceiveBytes,
					TxBytes:       p.TransmitBytes,
				}
				if p.Endpoint != nil {
					ps.Endpoint = p.Endpoint.String()
				}
				for _, ip := range p.AllowedIPs {
					ps.AllowedIPs = append(ps.AllowedIPs, ip.String())
				}
				ds.Peers = append(ds.Peers, ps)
			}
			return ds, nil
		}
	}
	return c.statsFromDump(ctx, iface)
}

func (c *Client) statsFromDump(ctx context.Context, iface string) (*DeviceStats, error) {
	res, err := c.runner.Run(ctx, "wg", "show", iface, "dump")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("wg show %s: %s", iface, res.Combined())
	}
	return ParseDump(iface, res.Stdout)
}

// ParseDump parses `wg show <iface> dump` output.
func ParseDump(iface, out string) (*DeviceStats, error) {
	ds := &DeviceStats{Name: iface}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if i == 0 && len(f) == 4 {
			// private-key public-key listen-port fwmark
			ds.ListenPort, _ = strconv.Atoi(f[2])
			continue
		}
		if len(f) < 8 {
			continue
		}
		// public-key preshared-key endpoint allowed-ips latest-handshake rx tx keepalive
		hs, _ := strconv.ParseInt(f[4], 10, 64)
		rx, _ := strconv.ParseInt(f[5], 10, 64)
		tx, _ := strconv.ParseInt(f[6], 10, 64)
		ps := PeerStats{PublicKey: f[0], RxBytes: rx, TxBytes: tx}
		if f[2] != "(none)" {
			ps.Endpoint = f[2]
		}
		if hs > 0 {
			ps.LastHandshake = time.Unix(hs, 0)
		}
		if f[3] != "(none)" {
			ps.AllowedIPs = strings.Split(f[3], ",")
		}
		ds.Peers = append(ds.Peers, ps)
	}
	return ds, nil
}

// InterfaceExists reports whether a network interface named iface exists.
func InterfaceExists(iface string) bool {
	ifs, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, i := range ifs {
		if i.Name == iface {
			return true
		}
	}
	return false
}

// TunnelAddrs returns the IP addresses assigned to iface.
func TunnelAddrs(iface string) []string {
	ifc, err := net.InterfaceByName(iface)
	if err != nil {
		return nil
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

// Up runs `wg-quick up <iface>`. Returns combined output and an error on failure.
func (c *Client) Up(ctx context.Context, iface string) (string, error) {
	return c.quick(ctx, "up", iface)
}

// Down runs `wg-quick down <iface>`.
func (c *Client) Down(ctx context.Context, iface string) (string, error) {
	return c.quick(ctx, "down", iface)
}

func (c *Client) quick(ctx context.Context, verb, iface string) (string, error) {
	if _, err := c.runner.LookPath("wg-quick"); err != nil {
		return "", fmt.Errorf("wg-quick not found: install wireguard-tools")
	}
	res, err := c.runner.Run(ctx, "wg-quick", verb, iface)
	if err != nil {
		return res.Combined(), err
	}
	if res.ExitCode != 0 {
		return res.Combined(), fmt.Errorf("wg-quick %s %s exited %d", verb, iface, res.ExitCode)
	}
	return res.Combined(), nil
}

// CheckBinaries verifies required external binaries exist.
func (c *Client) CheckBinaries() error {
	var missing []string
	for _, b := range []string{"wg", "wg-quick"} {
		if _, err := c.runner.LookPath(b); err != nil {
			missing = append(missing, b)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required binaries: %s (install wireguard-tools)", strings.Join(missing, ", "))
	}
	return nil
}

// ResolvConfDNS returns nameservers from /etc/resolv.conf (best effort).
func ResolvConfDNS() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				out = append(out, f[1])
			}
		}
	}
	return out
}
