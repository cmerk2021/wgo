// Package profile discovers and parses WireGuard profiles in /etc/wireguard.
package profile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dir is the WireGuard config directory. Variable so tests can override.
var Dir = "/etc/wireguard"

// Peer is a parsed [Peer] section.
type Peer struct {
	PublicKey  string
	Endpoint   string // host:port, host may be a hostname or IP
	AllowedIPs []string
}

// Profile is a parsed WireGuard configuration file.
type Profile struct {
	Name      string // interface name (filename without .conf)
	Path      string
	Addresses []string
	DNS       []string
	Peers     []Peer
	ParseErr  error // non-nil when the file is unreadable or invalid
}

// EndpointHost returns the host part of the first peer endpoint.
func (p *Profile) EndpointHost() string {
	if len(p.Peers) == 0 || p.Peers[0].Endpoint == "" {
		return ""
	}
	host, _ := splitHostPort(p.Peers[0].Endpoint)
	return host
}

// EndpointPort returns the port part of the first peer endpoint.
func (p *Profile) EndpointPort() string {
	if len(p.Peers) == 0 || p.Peers[0].Endpoint == "" {
		return ""
	}
	_, port := splitHostPort(p.Peers[0].Endpoint)
	return port
}

func splitHostPort(ep string) (host, port string) {
	// Handle [v6]:port.
	if strings.HasPrefix(ep, "[") {
		if i := strings.LastIndex(ep, "]:"); i >= 0 {
			return ep[1:i], ep[i+2:]
		}
		return strings.Trim(ep, "[]"), ""
	}
	if i := strings.LastIndex(ep, ":"); i >= 0 {
		return ep[:i], ep[i+1:]
	}
	return ep, ""
}

// List discovers profiles in Dir, sorted by name. A missing directory
// returns an empty list and a descriptive error.
func List() ([]Profile, error) {
	entries, err := os.ReadDir(Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s does not exist; install wireguard-tools or create it", Dir)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", Dir, err)
	}
	var out []Profile
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".conf") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		path := filepath.Join(Dir, e.Name())
		p, perr := ParseFile(path)
		if perr != nil {
			out = append(out, Profile{Name: name, Path: path, ParseErr: perr})
			continue
		}
		p.Name = name
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ParseFile parses a WireGuard .conf file.
func ParseFile(path string) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p, err := Parse(f)
	if err != nil {
		return nil, err
	}
	p.Path = path
	p.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return p, nil
}

// Parse parses WireGuard INI-style config from r.
func Parse(r io.Reader) (*Profile, error) {
	p := &Profile{}
	var section string
	var cur *Peer
	sc := bufio.NewScanner(r)
	lineNo := 0
	sawInterface := false
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			switch section {
			case "interface":
				sawInterface = true
			case "peer":
				p.Peers = append(p.Peers, Peer{})
				cur = &p.Peers[len(p.Peers)-1]
			default:
				return nil, fmt.Errorf("line %d: unknown section [%s]", lineNo, section)
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		switch section {
		case "interface":
			switch key {
			case "address":
				p.Addresses = append(p.Addresses, splitCSV(val)...)
			case "dns":
				p.DNS = append(p.DNS, splitCSV(val)...)
			}
		case "peer":
			if cur == nil {
				return nil, fmt.Errorf("line %d: key outside of section", lineNo)
			}
			switch key {
			case "publickey":
				cur.PublicKey = val
			case "endpoint":
				cur.Endpoint = val
			case "allowedips":
				cur.AllowedIPs = append(cur.AllowedIPs, splitCSV(val)...)
			}
		case "":
			return nil, fmt.Errorf("line %d: key outside of section", lineNo)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawInterface {
		return nil, errors.New("missing [Interface] section")
	}
	return p, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}
