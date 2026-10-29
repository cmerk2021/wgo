// Package killswitch manages firewall-based kill switch rules via nftables
// (preferred) or iptables/ip6tables (fallback).
package killswitch

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/cmerk2021/wgo/internal/execx"
)

// NFTTable is the dedicated nftables table name.
const NFTTable = "wgo"

// IPTChain is the dedicated iptables chain name.
const IPTChain = "WGO_KILLSWITCH"

// Rules describes the desired kill switch policy.
type Rules struct {
	Iface        string   // tunnel interface, always allowed
	EndpointIPs  []string // WireGuard endpoint IPs (v4 and/or v6)
	EndpointPort string   // WireGuard endpoint UDP port
	AllowLAN     bool
	LANRanges    []string // CIDR ranges to allow when AllowLAN
	AllowDHCP    bool
}

// Backend applies and removes kill switch rules. Implementations must be
// idempotent: Apply twice or Remove when absent must not error.
type Backend interface {
	Name() string
	Apply(ctx context.Context, r Rules) error
	Remove(ctx context.Context) error
	Active(ctx context.Context) (bool, error)
}

// Detect returns the first available backend (nftables, then iptables).
func Detect(runner execx.Runner) (Backend, error) {
	if _, err := runner.LookPath("nft"); err == nil {
		return &NFT{Runner: runner}, nil
	}
	if _, err := runner.LookPath("iptables"); err == nil {
		return &IPTables{Runner: runner}, nil
	}
	return nil, fmt.Errorf("kill switch unavailable: neither nft nor iptables found")
}

// RemoveAll force-removes rules from every backend (crash-recovery escape hatch).
func RemoveAll(ctx context.Context, runner execx.Runner) error {
	var errs []string
	if _, err := runner.LookPath("nft"); err == nil {
		if err := (&NFT{Runner: runner}).Remove(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if _, err := runner.LookPath("iptables"); err == nil {
		if err := (&IPTables{Runner: runner}).Remove(ctx); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// splitV4V6 partitions IP strings into v4 and v6 groups, dropping invalid ones.
func splitV4V6(ips []string) (v4, v6 []string) {
	for _, s := range ips {
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			v4 = append(v4, ip.String())
		} else {
			v6 = append(v6, ip.String())
		}
	}
	return
}

func splitRangesV4V6(ranges []string) (v4, v6 []string) {
	for _, s := range ranges {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		if ipnet.IP.To4() != nil {
			v4 = append(v4, ipnet.String())
		} else {
			v6 = append(v6, ipnet.String())
		}
	}
	return
}
