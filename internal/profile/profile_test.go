package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleConf = `
[Interface]
PrivateKey = cGxhY2Vob2xkZXJrZXkxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=
Address = 10.7.0.2/32, fd00::2/128
DNS = 10.7.0.1, 1.1.1.1

[Peer]
PublicKey = c2VydmVycHVibGlja2V5MTIzNDU2Nzg5MDEyMzQ1Njc4OTA=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
`

func TestParse(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleConf))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Addresses) != 2 || p.Addresses[0] != "10.7.0.2/32" {
		t.Errorf("addresses: %v", p.Addresses)
	}
	if len(p.DNS) != 2 || p.DNS[0] != "10.7.0.1" {
		t.Errorf("dns: %v", p.DNS)
	}
	if len(p.Peers) != 1 {
		t.Fatalf("peers: %v", p.Peers)
	}
	if p.Peers[0].Endpoint != "vpn.example.com:51820" {
		t.Errorf("endpoint: %s", p.Peers[0].Endpoint)
	}
	if p.EndpointHost() != "vpn.example.com" || p.EndpointPort() != "51820" {
		t.Errorf("endpoint split: %s %s", p.EndpointHost(), p.EndpointPort())
	}
	if len(p.Peers[0].AllowedIPs) != 2 {
		t.Errorf("allowedips: %v", p.Peers[0].AllowedIPs)
	}
}

func TestParseNoPeer(t *testing.T) {
	p, err := Parse(strings.NewReader("[Interface]\nAddress = 10.0.0.1/24\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Peers) != 0 {
		t.Errorf("expected no peers, got %v", p.Peers)
	}
	if p.EndpointHost() != "" {
		t.Errorf("expected empty endpoint host")
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		"Address = 10.0.0.1/24\n",         // key outside section
		"[Bogus]\nfoo = bar\n",            // unknown section
		"[Peer]\nPublicKey = x\n",         // missing [Interface]
		"[Interface]\nnot-a-kv-line\n",    // bad line
	}
	for _, c := range cases {
		if _, err := Parse(strings.NewReader(c)); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestIPv6Endpoint(t *testing.T) {
	conf := "[Interface]\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = k\nEndpoint = [2001:db8::1]:51820\n"
	p, err := Parse(strings.NewReader(conf))
	if err != nil {
		t.Fatal(err)
	}
	if p.EndpointHost() != "2001:db8::1" || p.EndpointPort() != "51820" {
		t.Errorf("v6 endpoint split: %q %q", p.EndpointHost(), p.EndpointPort())
	}
}

func TestListDiscovery(t *testing.T) {
	dir := t.TempDir()
	old := Dir
	Dir = dir
	defer func() { Dir = old }()

	os.WriteFile(filepath.Join(dir, "wg0.conf"), []byte(sampleConf), 0o600)
	os.WriteFile(filepath.Join(dir, "bad.conf"), []byte("garbage no sections"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600)

	profs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(profs) != 2 {
		t.Fatalf("expected 2 profiles, got %d: %+v", len(profs), profs)
	}
	// Sorted: bad, wg0
	if profs[0].Name != "bad" || profs[0].ParseErr == nil {
		t.Errorf("bad profile should carry ParseErr: %+v", profs[0])
	}
	if profs[1].Name != "wg0" || profs[1].ParseErr != nil {
		t.Errorf("wg0 should parse: %+v", profs[1])
	}
}

func TestListMissingDir(t *testing.T) {
	old := Dir
	Dir = "/nonexistent/wireguard-test"
	defer func() { Dir = old }()
	if _, err := List(); err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestImportRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	old := Dir
	Dir = dir
	defer func() { Dir = old }()

	src := filepath.Join(t.TempDir(), "wg9.conf")
	os.WriteFile(src, []byte(sampleConf), 0o644)

	p, err := Import(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "wg9" {
		t.Errorf("name: %s", p.Name)
	}
	fi, _ := os.Stat(filepath.Join(dir, "wg9.conf"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("imported file mode: %v", fi.Mode().Perm())
	}
	// Second import without overwrite must refuse.
	if _, err := Import(src, false); err == nil || !strings.Contains(err.Error(), "confirm overwrite") {
		t.Errorf("expected overwrite refusal, got %v", err)
	}
	// With overwrite it succeeds.
	if _, err := Import(src, true); err != nil {
		t.Errorf("overwrite import: %v", err)
	}
}

func TestImportInvalid(t *testing.T) {
	dir := t.TempDir()
	old := Dir
	Dir = dir
	defer func() { Dir = old }()
	src := filepath.Join(t.TempDir(), "bad.conf")
	os.WriteFile(src, []byte("nope"), 0o644)
	if _, err := Import(src, false); err == nil {
		t.Error("expected validation error")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.conf")); err == nil {
		t.Error("invalid profile must not be copied")
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wg0.conf")
	os.WriteFile(path, []byte(sampleConf), 0o600)
	bak, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	if bak != path+".bak" {
		t.Errorf("bak path: %s", bak)
	}
	data, err := os.ReadFile(bak)
	if err != nil || string(data) != sampleConf {
		t.Errorf("backup content mismatch: %v", err)
	}
}
