# wgo

A full-screen terminal UI and CLI for managing WireGuard tunnels on Linux.
`wgo` wraps `wg-quick` and `wg`, works with your existing
`/etc/wireguard/*.conf` files, and never modifies them (except through an
explicit, backed-up edit action). You can keep using `wg-quick` manually
alongside it.

```
┌ 1 Dashboard ┐ 2 Profiles  3 Throughput  4 Logs  5 Settings

  ● CONNECTED

    Profile      home
    Interface    home
    Endpoint     203.0.113.5:51820
    Tunnel IP    10.7.0.2/32
    Uptime       42m10s
    Handshake    1m2s ago
    Transfer     ↓ 1.2 GB  ↑ 88.4 MB
    Rate         ↓ 1.4 MB/s  ↑ 120.3 KB/s
    Kill switch  ON
```

## Install

Requirements: Linux, `wireguard-tools` (`wg`, `wg-quick`), and optionally
`nftables` or `iptables` for the kill switch.

### From source

```sh
git clone https://github.com/cmerk2021/wgo
cd wgo
make build          # -> bin/wgo
sudo make install   # -> /usr/local/bin/wgo
```

### Release binaries

```sh
make release        # static amd64 + arm64 binaries in dist/
```

Release builds are fully static (`CGO_ENABLED=0`).

## Usage

```
wgo                    launch the TUI
wgo up [profile]       connect with staged verification (default: last used)
wgo down [profile]     disconnect
wgo status [--json]    show current state
wgo list               list profiles in /etc/wireguard
wgo killswitch status  show kill switch state
wgo killswitch off     force-remove all kill switch rules (escape hatch)
wgo version            print version
```

Run `wgo` as a normal user: it auto-elevates via the first available of
`sudo`, `doas`, or `pkexec` (the password prompt appears in your terminal
before the TUI starts). If none are installed, it exits with a clear error.

### Typical workflow

1. Type `wgo`. The idle Dashboard appears.
2. Press `c` to connect with the most recently used profile, or press `2`
   to pick another on the Profiles tab.
3. Watch the staged checklist: **Interface up → Handshake → Data flowing →
   DNS / internet**. Only after every stage passes is the tunnel marked
   Connected.
4. Glance at the Dashboard for live stats.

Interfaces brought up manually with `wg-quick up` are detected at launch
and shown as Connected — wgo never assumes it started the tunnel.

## Key bindings

| Key        | Action                                                        |
|------------|---------------------------------------------------------------|
| `c`        | connect (last-used profile) / disconnect toggle               |
| `1`–`5`    | switch tabs (also `tab` / `shift+tab`)                        |
| `k`        | toggle kill switch for the current session                    |
| `t`        | run DNS leak / connectivity test on demand                    |
| `d`        | disconnect **and** quit (tears down tunnel + kill switch)     |
| `q`        | quit (dynamic — see below)                                    |
| `r` / `x`  | retry / tear down after a failed connect                      |
| `?`        | help overlay                                                  |

Profiles tab: `↑`/`↓` select, `enter` connect, `i` import a `.conf` by path,
`e` edit in `$EDITOR` (backs up to `<name>.conf.bak` first, re-validates on
return), `w`/`L`/`a` toggle kill switch / allow-LAN / auto-reconnect per
profile.

Settings tab: `↑`/`↓` select, `enter` toggle or edit a value. Changes are
written to `/etc/wgo/config.toml` immediately.

### Quit behavior

- **Kill switch off:** `q` exits immediately and leaves the tunnel running.
- **Kill switch on:** `q` asks: *"Quitting will remove the kill switch. The
  tunnel stays up. Quit anyway? [y/N]"*. On `y` the rules are removed and
  wgo exits, leaving the tunnel up. Anything else returns to the TUI.
- `d` always tears everything down (tunnel + kill switch) and quits.

## No background daemon (v1 limitation)

**wgo has no daemon.** Auto-reconnect and the kill switch are only managed
while the TUI (or a `wgo up` command) is running. If wgo exits, the tunnel
keeps running but nothing watches it, and kill switch rules left behind by
a crash can be removed at any time with:

```sh
sudo wgo killswitch off
```

## Files

| Path                     | Purpose                                            |
|--------------------------|----------------------------------------------------|
| `/etc/wireguard/*.conf`  | profiles (interface name = filename without .conf) |
| `/etc/wgo/config.toml`   | app config, created with defaults on first run     |
| `/var/lib/wgo/state.json`| runtime state (last-used profile)                  |

wgo never writes to existing profile `.conf` files except via the explicit
edit action on the Profiles tab, which backs up the original to
`<name>.conf.bak` first. Imports copy the file into `/etc/wireguard` with
mode `0600` after validating it, and refuse to overwrite without
confirmation.

## Configuration reference (`/etc/wgo/config.toml`)

```toml
[global]
quit_default = "dynamic"          # quit behavior (dynamic = confirm only with kill switch on)
handshake_stale_seconds = 180     # mark tunnel Stale past this handshake age
stage_timeout_seconds = 15        # per verification stage
dns_check_host = "example.com"    # host resolved in the DNS stage / leak test
probe_target = "1.1.1.1"          # ping/TCP connectivity probe target
public_ip_url = "https://api.ipify.org"  # informational public-IP check
lan_ranges = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
killswitch = false                # default kill switch behavior
allow_lan = true                  # default allow-LAN behavior
auto_reconnect = false            # default auto-reconnect behavior
reconnect_max_attempts = 5

[profiles.home]
killswitch = true
allow_lan = true
auto_reconnect = true

[profiles.work]
killswitch = false
auto_reconnect = true
probe_target = "10.0.0.1"         # per-profile probe override
# dns_check_host = "intranet.example"
```

Unknown or missing profile entries fall back to the global defaults.

## Staged verification

`wgo up` / pressing `c` runs `wg-quick up` in the background and verifies:

1. **Interface up** — `wg-quick` exited 0 and the interface exists.
2. **Handshake** — latest handshake is non-zero and recent.
3. **Data flowing** — rx bytes increased since bring-up.
4. **DNS / internet** — DNS lookup of `dns_check_host` succeeds and a
   connectivity probe (ping, falling back to TCP :443) to `probe_target`
   succeeds.

Each stage polls until it passes or times out (`stage_timeout_seconds`).
On failure the exact stage and captured `wg-quick` output are shown with
Retry (`r`) and Tear down (`x`) options.

A DNS leak check (resolution works, system resolver matches the profile's
`DNS =` line when determinable) and a public-IP comparison run after
connect and on demand (`t`). A failed public-IP lookup is informational
only and never fails the connection.

## Auto-reconnect

While connected, wgo monitors handshake age (WireGuard rekeys about every
2 minutes under traffic). Past `handshake_stale_seconds` the tunnel is
marked **Stale**; with auto-reconnect enabled wgo recovers with
`wg-quick down` + `up`, exponential backoff (2s, 4s, 8s, … capped at 2m),
and a max attempts limit. State and attempt counts appear on the Dashboard
and in the Logs. If the endpoint IP changed, kill switch rules are updated
from the live endpoint after reconnect.

## Kill switch safety

- Implementation: **nftables first** — a dedicated `inet wgo` table, so
  cleanup is a single `nft delete table inet wgo`. Falls back to
  iptables/ip6tables with a dedicated `WGO_KILLSWITCH` chain. Auto-detected.
- It never touches your other firewall rules.
- Allowed: loopback, the tunnel interface, the WireGuard endpoint IP(s) and
  UDP port (so the tunnel can connect and reconnect), established/related
  traffic, DHCP, and optionally LAN ranges (plus link-local). Everything
  else on output is dropped, for both IPv4 and IPv6.
- Endpoint hostnames are resolved before bring-up, then refreshed from the
  live endpoint (wgctrl) once the interface is up.
- Removed on disconnect, on a confirmed quit, and on SIGINT/SIGTERM/SIGHUP.
  Applying twice or removing when absent is safe (idempotent).
- **Crash recovery:** `sudo wgo killswitch off` removes the nft table and
  iptables chain even when wgo isn't running.

## Development

```sh
make build     # build bin/wgo
make test      # go test ./...
make vet       # go vet
make release   # static amd64 + arm64 in dist/
```

All external commands (`wg-quick`, `wg`, `nft`, `iptables`, `ip`) go
through a mockable runner (`internal/execx`), so the verification engine,
kill switch rule generation, and quit flow are unit tested without
touching the system.

## Assumptions

Defaults chosen where the spec left room:

- **Elevation order:** `sudo` → `doas` → `pkexec`, passing original args;
  the environment is preserved (so `$EDITOR` keeps working).
- **Handshake "recent"** during verification means under 3 minutes.
- **Connectivity probe:** `ping -c 1 -W 3` when `ping` exists, otherwise a
  TCP dial to `probe_target:443`. ICMP raw sockets are avoided for
  simplicity even though wgo runs as root.
- **Throughput graphs** are custom block-character sparklines (no chart
  dependency) over a rolling ~120 s window sampled once per second.
- **Reconnect backoff:** 2s · 2^(attempt−1), capped at 2 minutes, default
  max 5 attempts (`reconnect_max_attempts`).
- **Uptime for externally started tunnels** counts from when wgo detected
  them (the kernel doesn't expose bring-up time).
- **Multiple active interfaces:** wgo manages/display the first one
  (sorted) as the active tunnel; `wgo status` lists all of them.
- **DHCP** is always allowed through the kill switch (v4 ports 67/68, v6
  546/547).
- **Logs** are kept in-memory only (last 500 lines), not written to disk.
- **Profile navigation `L`** (capital) toggles allow-LAN because lowercase
  `l`/`k` are reserved (`k` = kill switch).
- **Kill switch pre-connect:** when enabled for a profile, rules are
  applied *before* `wg-quick up` (endpoint pre-resolved) so no traffic
  leaks during the connect window.
- **Config/state write permissions:** `/etc/wgo` and `/var/lib/wgo` are
  created `0755` with `0644` files; profiles are imported `0600`.

## License

MIT — see [LICENSE](LICENSE).
