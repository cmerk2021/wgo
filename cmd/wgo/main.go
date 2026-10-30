// Command wgo is a TUI and CLI manager for WireGuard tunnels.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmerk2021/wgo/internal/config"
	"github.com/cmerk2021/wgo/internal/elevate"
	"github.com/cmerk2021/wgo/internal/execx"
	"github.com/cmerk2021/wgo/internal/killswitch"
	"github.com/cmerk2021/wgo/internal/monitor"
	"github.com/cmerk2021/wgo/internal/profile"
	"github.com/cmerk2021/wgo/internal/tui"
	"github.com/cmerk2021/wgo/internal/verify"
	"github.com/cmerk2021/wgo/internal/wg"
)

var version = "0.1.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println("wgo " + version)
			return
		case "help", "--help", "-h":
			usage()
			return
		}
	}

	if err := elevate.EnsureRoot(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	runner := execx.NewSystem()
	client := wg.New(runner)
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err, "(using defaults)")
	}
	st, _ := config.LoadState()
	ctrl := monitor.NewController(cfg, client, runner)

	if len(os.Args) < 2 {
		os.Exit(runTUI(cfg, st, ctrl, client))
	}

	switch os.Args[1] {
	case "up":
		os.Exit(cmdUp(cfg, st, ctrl, client, os.Args[2:]))
	case "down":
		os.Exit(cmdDown(st, ctrl, os.Args[2:]))
	case "status":
		os.Exit(cmdStatus(client, ctrl, runner, os.Args[2:]))
	case "list":
		os.Exit(cmdList())
	case "killswitch":
		os.Exit(cmdKillswitch(runner, os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println(`wgo — WireGuard TUI manager

Usage:
  wgo                    launch the TUI
  wgo up [profile]       connect (staged verification), default: last used
  wgo down [profile]     disconnect
  wgo status [--json]    show current state
  wgo list               list profiles in /etc/wireguard
  wgo killswitch status  show kill switch state
  wgo killswitch off     force-remove all kill switch rules (escape hatch)
  wgo version            print version`)
}

// cleanupOnSignals removes kill switch rules on SIGINT/SIGTERM/SIGHUP.
func cleanupOnSignals(ctrl *monitor.Controller, after func()) chan<- struct{} {
	sigc := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case <-sigc:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = ctrl.DisableKillswitch(ctx)
			if after != nil {
				after()
			}
			os.Exit(130)
		case <-done:
			signal.Stop(sigc)
		}
	}()
	return done
}

func runTUI(cfg *config.Config, st *config.State, ctrl *monitor.Controller, client *wg.Client) int {
	if err := client.CheckBinaries(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	m := tui.New(cfg, st, ctrl, client)
	m.DetectExisting()
	p := tea.NewProgram(m, tea.WithAltScreen())
	done := cleanupOnSignals(ctrl, func() { p.Kill() })
	_, err := p.Run()
	close(done)
	m.Shutdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func pickProfile(st *config.State, args []string) (*profile.Profile, error) {
	profs, err := profile.List()
	if err != nil {
		return nil, err
	}
	name := st.LastProfile
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		if len(profs) == 1 {
			return &profs[0], nil
		}
		return nil, fmt.Errorf("no profile specified and no last-used profile; run: wgo up <profile>")
	}
	for i := range profs {
		if profs[i].Name == name {
			if profs[i].ParseErr != nil {
				return nil, fmt.Errorf("profile %s: %v", name, profs[i].ParseErr)
			}
			return &profs[i], nil
		}
	}
	return nil, fmt.Errorf("profile %q not found in %s", name, profile.Dir)
}

func cmdUp(cfg *config.Config, st *config.State, ctrl *monitor.Controller, client *wg.Client, args []string) int {
	if err := client.CheckBinaries(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	prof, err := pickProfile(st, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	ctrl.OnEvent = func(e monitor.Event) {
		fmt.Printf("  %s %s\n", e.At.Format("15:04:05"), e.Message)
	}
	ctrl.OnStage = func(e monitor.StageEvent) {
		if e.Running {
			fmt.Printf("… %s\n", e.Stage)
		} else if e.Result != nil {
			mark := "✓"
			if !e.Result.OK {
				mark = "✗"
			}
			fmt.Printf("%s %s  %s\n", mark, e.Stage, e.Result.Detail)
		}
	}
	done := cleanupOnSignals(ctrl, nil)
	defer close(done)

	st.LastProfile = prof.Name
	_ = st.Save()
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.Global.StageTimeoutSeconds)*time.Second*time.Duration(len(verify.Stages)+2))
	defer cancel()
	if err := ctrl.Connect(ctx, *prof); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "the interface may still be up; run `wgo down` to tear it down")
		return 1
	}
	fmt.Printf("connected: %s\n", prof.Name)
	if ctrl.KillswitchActive() {
		fmt.Println("note: the kill switch stays active until `wgo down` or `wgo killswitch off`")
	}
	return 0
}

func cmdDown(st *config.State, ctrl *monitor.Controller, args []string) int {
	name := st.LastProfile
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "error: no profile specified and no last-used profile")
		return 1
	}
	ctrl.OnEvent = func(e monitor.Event) {
		fmt.Printf("  %s %s\n", e.At.Format("15:04:05"), e.Message)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ctrl.Disconnect(ctx, name); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("disconnected: %s\n", name)
	return 0
}

type statusOut struct {
	Connected  bool     `json:"connected"`
	Interfaces []string `json:"interfaces"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Handshake  string   `json:"last_handshake,omitempty"`
	RxBytes    int64    `json:"rx_bytes"`
	TxBytes    int64    `json:"tx_bytes"`
	Killswitch bool     `json:"killswitch"`
}

func cmdStatus(client *wg.Client, ctrl *monitor.Controller, runner execx.Runner, args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output JSON")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out := statusOut{}
	ifaces, _ := client.ActiveInterfaces(ctx)
	out.Interfaces = ifaces
	out.Connected = len(ifaces) > 0
	if out.Connected {
		if ds, err := client.Stats(ctx, ifaces[0]); err == nil {
			out.Endpoint = ds.Endpoint()
			if hs := ds.LatestHandshake(); !hs.IsZero() {
				out.Handshake = hs.Format(time.RFC3339)
			}
			out.RxBytes = ds.TotalRx()
			out.TxBytes = ds.TotalTx()
		}
	}
	if b, err := killswitch.Detect(runner); err == nil {
		active, _ := b.Active(ctx)
		out.Killswitch = active
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	if !out.Connected {
		fmt.Println("status: disconnected")
	} else {
		fmt.Printf("status: connected (%v)\n", out.Interfaces)
		if out.Endpoint != "" {
			fmt.Printf("endpoint: %s\n", out.Endpoint)
		}
		if out.Handshake != "" {
			fmt.Printf("last handshake: %s\n", out.Handshake)
		}
		fmt.Printf("transfer: ↓ %d B  ↑ %d B\n", out.RxBytes, out.TxBytes)
	}
	fmt.Printf("killswitch: %v\n", out.Killswitch)
	return 0
}

func cmdList() int {
	profs, err := profile.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(profs) == 0 {
		fmt.Println("no profiles found in", profile.Dir)
		return 0
	}
	for _, p := range profs {
		status := ""
		if p.ParseErr != nil {
			status = "  (invalid: " + p.ParseErr.Error() + ")"
		} else if ep := p.EndpointHost(); ep != "" {
			status = "  endpoint " + ep
		}
		fmt.Printf("%s%s\n", p.Name, status)
	}
	return 0
}

func cmdKillswitch(runner execx.Runner, args []string) int {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	switch sub {
	case "off":
		if err := killswitch.RemoveAll(ctx, runner); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		fmt.Println("all kill switch rules removed")
		return 0
	case "status":
		b, err := killswitch.Detect(runner)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		active, err := b.Active(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		if active {
			fmt.Printf("killswitch: active (%s)\n", b.Name())
		} else {
			fmt.Println("killswitch: inactive")
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown killswitch subcommand %q (use: off, status)\n", sub)
		return 2
	}
}
