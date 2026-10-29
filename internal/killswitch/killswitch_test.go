package killswitch

import (
	"context"
	"strings"
	"testing"

	"github.com/cmerk2021/wgo/internal/execx"
)

var testRules = Rules{
	Iface:        "wg0",
	EndpointIPs:  []string{"203.0.113.5", "2001:db8::5", "not-an-ip"},
	EndpointPort: "51820",
	AllowLAN:     true,
	LANRanges:    []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fd00::/8", "bogus"},
	AllowDHCP:    true,
}

func TestNFTRuleset(t *testing.T) {
	rs := Ruleset(testRules)
	for _, want := range []string{
		"table inet wgo {",
		"delete table inet wgo",
		"policy drop",
		`oifname "lo" accept`,
		`oifname "wg0" accept`,
		"ip daddr 203.0.113.5 udp dport 51820 accept",
		"ip6 daddr 2001:db8::5 udp dport 51820 accept",
		"ct state established,related accept",
		"ip daddr 10.0.0.0/8 accept",
		"ip daddr 172.16.0.0/12 accept",
		"ip daddr 192.168.0.0/16 accept",
		"ip6 daddr fd00::/8 accept",
		"ip daddr 169.254.0.0/16 accept",
		"ip6 daddr fe80::/10 accept",
		"udp dport { 67, 68 } accept",
		"counter drop",
	} {
		if !strings.Contains(rs, want) {
			t.Errorf("ruleset missing %q\n%s", want, rs)
		}
	}
	if strings.Contains(rs, "not-an-ip") || strings.Contains(rs, "bogus") {
		t.Error("invalid IPs/CIDRs must be dropped from the ruleset")
	}
}

func TestNFTRulesetNoLAN(t *testing.T) {
	r := testRules
	r.AllowLAN = false
	rs := Ruleset(r)
	if strings.Contains(rs, "10.0.0.0/8") {
		t.Error("LAN ranges must be absent when AllowLAN is false")
	}
}

func TestNFTRulesetDefaultPort(t *testing.T) {
	r := testRules
	r.EndpointPort = ""
	rs := Ruleset(r)
	if !strings.Contains(rs, "udp dport 51820 accept") {
		t.Error("expected default port 51820")
	}
}

func TestIPTablesCommands(t *testing.T) {
	cmds := IPTablesCommands(testRules)
	var rendered []string
	for _, c := range cmds {
		rendered = append(rendered, strings.Join(c, " "))
	}
	all := strings.Join(rendered, "\n")
	for _, want := range []string{
		"iptables -N WGO_KILLSWITCH",
		"iptables -F WGO_KILLSWITCH",
		"iptables -A WGO_KILLSWITCH -o lo -j ACCEPT",
		"iptables -A WGO_KILLSWITCH -o wg0 -j ACCEPT",
		"iptables -A WGO_KILLSWITCH -d 203.0.113.5 -p udp --dport 51820 -j ACCEPT",
		"ip6tables -A WGO_KILLSWITCH -d 2001:db8::5 -p udp --dport 51820 -j ACCEPT",
		"iptables -A WGO_KILLSWITCH -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"iptables -A WGO_KILLSWITCH -d 10.0.0.0/8 -j ACCEPT",
		"ip6tables -A WGO_KILLSWITCH -d fd00::/8 -j ACCEPT",
		"iptables -A WGO_KILLSWITCH -d 169.254.0.0/16 -j ACCEPT",
		"ip6tables -A WGO_KILLSWITCH -d fe80::/10 -j ACCEPT",
		"iptables -A WGO_KILLSWITCH -j DROP",
		"iptables -I OUTPUT 1 -j WGO_KILLSWITCH",
		"ip6tables -I OUTPUT 1 -j WGO_KILLSWITCH",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("commands missing %q\n%s", want, all)
		}
	}
	// v4 endpoint must never appear in ip6tables and vice versa.
	for _, r := range rendered {
		if strings.HasPrefix(r, "ip6tables") && strings.Contains(r, "203.0.113.5") {
			t.Errorf("v4 address in ip6tables: %s", r)
		}
		if strings.HasPrefix(r, "iptables") && strings.Contains(r, "2001:db8::5") {
			t.Errorf("v6 address in iptables: %s", r)
		}
	}
}

func TestNFTApplyAndRemoveIdempotent(t *testing.T) {
	m := execx.NewMock()
	n := &NFT{Runner: m}
	ctx := context.Background()
	if err := n.Apply(ctx, testRules); err != nil {
		t.Fatal(err)
	}
	if err := n.Apply(ctx, testRules); err != nil {
		t.Fatalf("second apply must not error: %v", err)
	}
	// Remove when absent: nft errors with "No such file or directory".
	m.Respond("nft delete table inet wgo", execx.Result{ExitCode: 1, Stderr: "Error: No such file or directory"})
	if err := n.Remove(ctx); err != nil {
		t.Fatalf("remove-when-absent must not error: %v", err)
	}
	calls := m.CallStrings()
	if calls[0] != "nft -f -" {
		t.Errorf("expected nft -f - apply, got %v", calls)
	}
}

func TestIPTablesRemoveIdempotent(t *testing.T) {
	m := execx.NewMock()
	// -D fails immediately (chain absent).
	m.Respond("iptables -D OUTPUT", execx.Result{ExitCode: 1, Stderr: "No chain"})
	m.Respond("ip6tables -D OUTPUT", execx.Result{ExitCode: 1, Stderr: "No chain"})
	i := &IPTables{Runner: m}
	if err := i.Remove(context.Background()); err != nil {
		t.Fatalf("remove-when-absent must not error: %v", err)
	}
}

func TestDetectPrefersNFT(t *testing.T) {
	m := execx.NewMock()
	b, err := Detect(m)
	if err != nil || b.Name() != "nftables" {
		t.Errorf("expected nftables, got %v %v", b, err)
	}
	m.SetMissing("nft")
	b, err = Detect(m)
	if err != nil || b.Name() != "iptables" {
		t.Errorf("expected iptables fallback, got %v %v", b, err)
	}
	m.SetMissing("iptables")
	if _, err := Detect(m); err == nil {
		t.Error("expected error when no backend available")
	}
}
