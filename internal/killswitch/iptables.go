package killswitch

import (
	"context"
	"fmt"
	"strings"

	"github.com/cmerk2021/wgo/internal/execx"
)

// IPTables implements Backend using iptables/ip6tables with a dedicated chain.
type IPTables struct {
	Runner execx.Runner
}

func (i *IPTables) Name() string { return "iptables" }

// IPTablesCommands generates the apply commands for r as argv slices
// (pure, for tests). Family "iptables" handles v4, "ip6tables" v6.
func IPTablesCommands(r Rules) [][]string {
	var cmds [][]string
	add := func(bin string, args ...string) {
		cmds = append(cmds, append([]string{bin}, args...))
	}
	port := r.EndpointPort
	if port == "" {
		port = "51820"
	}
	v4, v6 := splitV4V6(r.EndpointIPs)
	lan4, lan6 := splitRangesV4V6(r.LANRanges)

	for _, bin := range []string{"iptables", "ip6tables"} {
		// Fresh dedicated chain.
		add(bin, "-N", IPTChain)
		add(bin, "-F", IPTChain)
		add(bin, "-A", IPTChain, "-o", "lo", "-j", "ACCEPT")
		if r.Iface != "" {
			add(bin, "-A", IPTChain, "-o", r.Iface, "-j", "ACCEPT")
		}
		eps := v4
		lans := lan4
		if bin == "ip6tables" {
			eps = v6
			lans = lan6
		}
		for _, ip := range eps {
			add(bin, "-A", IPTChain, "-d", ip, "-p", "udp", "--dport", port, "-j", "ACCEPT")
		}
		add(bin, "-A", IPTChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")
		if r.AllowDHCP {
			if bin == "iptables" {
				add(bin, "-A", IPTChain, "-p", "udp", "--dport", "67:68", "-j", "ACCEPT")
			} else {
				add(bin, "-A", IPTChain, "-p", "udp", "--dport", "546:547", "-j", "ACCEPT")
			}
		}
		if r.AllowLAN {
			for _, cidr := range lans {
				add(bin, "-A", IPTChain, "-d", cidr, "-j", "ACCEPT")
			}
			if bin == "iptables" {
				add(bin, "-A", IPTChain, "-d", "169.254.0.0/16", "-j", "ACCEPT")
			} else {
				add(bin, "-A", IPTChain, "-d", "fe80::/10", "-j", "ACCEPT")
			}
		}
		add(bin, "-A", IPTChain, "-j", "DROP")
		// Jump from OUTPUT (inserted only if absent; see Apply).
		add(bin, "-I", "OUTPUT", "1", "-j", IPTChain)
	}
	return cmds
}

// Apply installs the chain and jump. Idempotent: existing chains are
// flushed and the OUTPUT jump is only inserted when absent.
func (i *IPTables) Apply(ctx context.Context, r Rules) error {
	for _, cmd := range IPTablesCommands(r) {
		bin, args := cmd[0], cmd[1:]
		// Skip duplicate OUTPUT jumps.
		if len(args) >= 2 && args[0] == "-I" && args[1] == "OUTPUT" {
			chk, _ := i.Runner.Run(ctx, bin, "-C", "OUTPUT", "-j", IPTChain)
			if chk.ExitCode == 0 {
				continue
			}
		}
		res, err := i.Runner.Run(ctx, bin, args...)
		if err != nil {
			return err
		}
		// -N on an existing chain fails; that's fine, -F follows.
		if res.ExitCode != 0 && args[0] != "-N" {
			return fmt.Errorf("%s %s: %s", bin, strings.Join(args, " "), res.Combined())
		}
	}
	return nil
}

// Remove deletes the jump and chain for both families. Missing rules are
// not errors.
func (i *IPTables) Remove(ctx context.Context) error {
	for _, bin := range []string{"iptables", "ip6tables"} {
		if _, err := i.Runner.LookPath(bin); err != nil {
			continue
		}
		// Remove all OUTPUT jumps (may have duplicates from crashes).
		for {
			res, err := i.Runner.Run(ctx, bin, "-D", "OUTPUT", "-j", IPTChain)
			if err != nil || res.ExitCode != 0 {
				break
			}
		}
		i.Runner.Run(ctx, bin, "-F", IPTChain)
		i.Runner.Run(ctx, bin, "-X", IPTChain)
	}
	return nil
}

// Active reports whether the chain exists in the v4 table.
func (i *IPTables) Active(ctx context.Context) (bool, error) {
	res, err := i.Runner.Run(ctx, "iptables", "-S", IPTChain)
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}
