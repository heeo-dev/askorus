// Command asksemble-agent is the Asksemble PC agent.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/api"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/devices"
	"github.com/asksemble/asksemble/agent/internal/logx"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usage = `Asksemble PC agent (not an official Anthropic product)

Usage:
  asksemble-agent serve [-port N]        start the API on 127.0.0.1
  asksemble-agent sessions [-state S] [-q TEXT] [-folder DIR] [-json]
                                         print the session list
  asksemble-agent info                   print what the agent can do
  asksemble-agent devices list           list paired devices
  asksemble-agent devices add NAME       add a device and print its token once
  asksemble-agent devices revoke ID|NAME remove a device
  asksemble-agent version

Settings live in %s (override with ASKSEMBLE_AGENT_DIR).
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(out, usage, dir)
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, "asksemble-agent", version)
		return nil
	case "serve":
		return serve(dir, args[1:])
	case "sessions":
		return sessions(dir, args[1:], out)
	case "info":
		return info(dir, out)
	case "devices":
		return devicesCmd(dir, args[1:], out)
	}
	return fmt.Errorf("unknown command %q (try: asksemble-agent help)", args[0])
}

func newAdapter(cfg config.Config) (*claude.Adapter, error) {
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		return nil, err
	}
	return claude.New(claude.Options{ClaudeDir: claudeDir, ClaudePath: cfg.ClaudePath}), nil
}

func serve(dir string, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 0, "port on 127.0.0.1 (default from settings)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Port = *port
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	logger, closeLog, err := openLog(dir)
	if err != nil {
		return err
	}
	defer closeLog()
	store, err := devices.Open(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	srv := api.HTTPServer(api.New(api.Options{
		Version:        version,
		Hostname:       host,
		Adapters:       []core.Adapter{adapter},
		Devices:        store,
		Logger:         logger,
		ServeHosts:     cfg.ServeHosts,
		TailscaleOwner: cfg.TailscaleOwner,
		HiddenFolders:  cfg.HiddenFolders,
	}).Handler())
	ln, err := api.Listen(cfg.Port)
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", cfg.Port, err)
	}
	if len(store.List()) == 0 {
		fmt.Fprintln(os.Stderr, "no paired devices yet: every request will get 401 until one is added")
	}
	logger.Info("listening", "addr", ln.Addr().String(), "version", version)
	fmt.Fprintf(os.Stderr, "asksemble-agent %s listening on http://%s (Ctrl+C to stop)\n", version, ln.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go adapter.Warm(ctx)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logger.Info("stopping")
	return srv.Shutdown(shutdown)
}

// openLog writes JSON lines to stderr and <dir>/logs/agent.log.
func openLog(dir string) (*slog.Logger, func(), error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(logDir, "agent.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return logx.New(io.MultiWriter(os.Stderr, f), slog.LevelInfo), func() { f.Close() }, nil
}

func sessions(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	state := fs.String("state", "", "comma-separated states: running,stopped,terminal,history,unknown")
	q := fs.String("q", "", "search title, name and last prompt")
	folder := fs.String("folder", "", "only sessions in this folder or below")
	asJSON := fs.Bool("json", false, "print JSON like GET /v1/sessions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	f := core.Filter{Query: *q, Folder: *folder, Hidden: cfg.HiddenFolders}
	if *state != "" {
		for _, s := range strings.Split(*state, ",") {
			st := core.State(strings.TrimSpace(s))
			if !core.ValidState(st) {
				return fmt.Errorf("unknown state %q", s)
			}
			f.States = append(f.States, st)
		}
	}
	start := time.Now()
	res, err := adapter.ListSessions(context.Background())
	if err != nil {
		return err
	}
	list := f.Apply(res.Sessions)
	took := time.Since(start)

	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(api.SessionsResponse{Sessions: list, Warnings: append([]string{}, res.Warnings...)})
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tLAST ACTIVE\tTITLE\tFOLDER")
	for _, s := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.State, s.LastActive.Local().Format("2006-01-02 15:04"), short(s.Title, 40), s.Folder)
	}
	tw.Flush()
	fmt.Fprintf(out, "\n%d sessions in %d ms", len(list), took.Milliseconds())
	if len(res.Warnings) > 0 {
		fmt.Fprintf(out, " (warnings: %s)", strings.Join(res.Warnings, ", "))
	}
	fmt.Fprintln(out)
	return nil
}

func info(dir string, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	adapter, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(adapter.Info(context.Background()))
}

func devicesCmd(dir string, args []string, out io.Writer) error {
	store, err := devices.Open(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPAIRED")
		for _, d := range store.List() {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", d.ID, d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		return tw.Flush()
	case "add":
		if len(args) != 2 {
			return errors.New("usage: asksemble-agent devices add NAME")
		}
		tok, d, err := store.Add(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "added %s (%s). Token, shown only now:\n%s\n", d.Name, d.ID, tok)
		return nil
	case "revoke":
		if len(args) != 2 {
			return errors.New("usage: asksemble-agent devices revoke ID|NAME")
		}
		d, err := store.Revoke(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "revoked %s (%s)\n", d.Name, d.ID)
		return nil
	}
	return fmt.Errorf("unknown devices command %q", args[0])
}

func short(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
