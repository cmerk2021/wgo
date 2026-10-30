package killswitch

import (
	"context"
	"fmt"
	"strings"

	"github.com/cmerk2021/wgo/internal/execx"
)

// NFT implements Backend using nftables with a dedicated `inet wgo` table.
type NFT struct {
	Runner execx.Runner
}

func (n *NFT) Name() string { return "nftables" }

// Ruleset generates the full nftables ruleset for r (pure, for tests).
func Ruleset(r Rules) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("table inet %s", NFTTable)
	w("delete table inet %s", NFTTable)
	w("table inet %s {", NFTTable)
	w("  chain output {")
	w("    type filter hook output priority 0; policy drop;")
	w("    oifname \"lo\" accept")
	if r.Iface != "" {
		w("    oifname %q accept", r.Iface)
	}
	v4, v6 := splitV4V6(r.EndpointIPs)
	port := r.EndpointPort
	if port == "" {
		port = "51820"
	}
	for _, ip := range v4 {
		w("    ip daddr %s udp dport %s accept", ip, port)
	}
	for _, ip := range v6 {
		w("    ip6 daddr %s udp dport %s accept", ip, port)
	}
	w("    ct state established,related accept")
	if r.AllowDHCP {
		w("    udp dport { 67, 68 } accept")
		w("    udp dport { 546, 547 } accept")
	}
	if r.AllowLAN {
		lan4, lan6 := splitRangesV4V6(r.LANRanges)
		for _, cidr := range lan4 {
			w("    ip daddr %s accept", cidr)
		}
		for _, cidr := range lan6 {
			w("    ip6 daddr %s accept", cidr)
		}
		w("    ip daddr 169.254.0.0/16 accept")
		w("    ip6 daddr fe80::/10 accept")
	}
	w("    counter drop")
	w("  }")
	w("}")
	return b.String()
}

// Apply installs the ruleset atomically via `nft -f -`. Idempotent: the
// ruleset recreates the table from scratch each time.
func (n *NFT) Apply(ctx context.Context, r Rules) error {
	res, err := n.Runner.RunInput(ctx, Ruleset(r), "nft", "-f", "-")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("nft apply failed: %s", res.Combined())
	}
	return nil
}

// Remove deletes the dedicated table. Missing table is not an error.
func (n *NFT) Remove(ctx context.Context) error {
	res, err := n.Runner.Run(ctx, "nft", "delete", "table", "inet", NFTTable)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 && !strings.Contains(res.Combined(), "No such file") {
		return fmt.Errorf("nft remove failed: %s", res.Combined())
	}
	return nil
}

// Active reports whether the wgo table exists.
func (n *NFT) Active(ctx context.Context) (bool, error) {
	res, err := n.Runner.Run(ctx, "nft", "list", "table", "inet", NFTTable)
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}
