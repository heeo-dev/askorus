// Command asksemble-agent is the Asksemble PC agent.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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
	"github.com/asksemble/asksemble/agent/internal/pairing"
	"github.com/asksemble/asksemble/agent/internal/tailnet"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usage = `Asksemble PC agent (not an official Anthropic product)

Usage:
  asksemble-agent serve [-port N]        start the API on 127.0.0.1
  asksemble-agent pair [-host H] [-port N] [-light-terminal]
                                         show a QR code for 5 minutes to pair a phone
  asksemble-agent sessions [-state S] [-q TEXT] [-folder DIR] [-json]
                                         print the session list
  asksemble-agent info                   print what the agent can do
  asksemble-agent devices [list]         list paired devices
  asksemble-agent devices revoke ID|NAME disconnect a device
  asksemble-agent version

Settings live in %s (override with ASKSEMBLE_AGENT_DIR).
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out io.Writer) error {
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
	case "pair":
		return pair(dir, args[1:], in, out)
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

// tailnetInfo is what serve and pair learn from the tailscale CLI.
type tailnetInfo struct {
	status  tailnet.Status
	serve   tailnet.Serve
	served  bool // tailscale serve proxies to our port
	problem string
}

func lookTailnet(ctx context.Context, localPort int) tailnetInfo {
	c, err := tailnet.New()
	if err != nil {
		return tailnetInfo{problem: "tailscale CLI not found"}
	}
	var ti tailnetInfo
	if ti.status, err = c.Status(ctx); err != nil {
		ti.problem = "tailscale status failed"
		return ti
	}
	if !ti.status.Running {
		ti.problem = "tailscale is not connected"
		return ti
	}
	if ti.serve, ti.served, err = c.ServeFor(ctx, localPort); err != nil {
		ti.problem = "tailscale serve status failed"
	}
	return ti
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Tailscale tells us the owner account and our MagicDNS name. Without
	// it the API still works locally, but phones cannot pair.
	ti := lookTailnet(ctx, cfg.Port)
	if ti.served && ti.serve.Funnel {
		return fmt.Errorf("tailscale funnel exposes this port to the internet; turn it off first (tailscale funnel --https=%d off)", ti.serve.HTTPSPort)
	}
	owner := cfg.TailscaleOwner
	if owner == "" {
		owner = ti.status.OwnerLogin
	}
	hosts := append([]string{}, cfg.ServeHosts...)
	if ti.status.DNSName != "" {
		hosts = append(hosts, ti.status.DNSName)
	}
	if ti.problem != "" {
		fmt.Fprintf(os.Stderr, "warning: %s; phones cannot pair until it is fixed\n", ti.problem)
	} else if !ti.served {
		fmt.Fprintf(os.Stderr, "note: tailscale serve does not point at 127.0.0.1:%d yet, so phones cannot reach the agent\n", cfg.Port)
	}

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
		Pairer:         &pairing.Pairer{Dir: dir, Devices: store},
		Logger:         logger,
		ServeHosts:     hosts,
		TailscaleOwner: owner,
		HiddenFolders:  cfg.HiddenFolders,
	}).Handler())
	ln, err := api.Listen(cfg.Port)
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%d: %w", cfg.Port, err)
	}
	if list, _ := store.List(); len(list) == 0 {
		fmt.Fprintln(os.Stderr, "no paired devices yet: run `asksemble-agent pair` in another window")
	}
	logger.Info("listening", "addr", ln.Addr().String(), "version", version, "tailscale_served", ti.served)
	fmt.Fprintf(os.Stderr, "asksemble-agent %s listening on http://%s (Ctrl+C to stop)\n", version, ln.Addr())

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

// PairURL is what the QR code holds. The app also opens it as a deep link.
func PairURL(host string, port int, code string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("host", host)
	q.Set("port", strconv.Itoa(port))
	q.Set("code", code)
	return "asksemble://pair?" + q.Encode()
}

func pair(dir string, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	hostFlag := fs.String("host", "", "MagicDNS name the phone connects to (default from tailscale)")
	portFlag := fs.Int("port", 0, "HTTPS port of tailscale serve (default from tailscale serve status)")
	light := fs.Bool("light-terminal", false, "draw the QR code for a light terminal background")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)), time.Second)
	if err != nil {
		return fmt.Errorf("the agent is not running on 127.0.0.1:%d; start `asksemble-agent serve` first", cfg.Port)
	}
	conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	host, port := *hostFlag, *portFlag
	if host == "" || port == 0 {
		ti := lookTailnet(ctx, cfg.Port)
		if ti.problem != "" {
			return errors.New(ti.problem)
		}
		if !ti.served {
			return fmt.Errorf("tailscale serve does not point at the agent yet. Pick a free HTTPS port and run:\n  tailscale serve --bg --https=<port> http://127.0.0.1:%d", cfg.Port)
		}
		if ti.serve.Funnel {
			return errors.New("tailscale funnel is on for the agent port; turn it off first")
		}
		if host == "" {
			host = ti.serve.Host
		}
		if port == 0 {
			port = ti.serve.HTTPSPort
		}
	}

	code, w, err := pairing.Open(dir, time.Now())
	if err != nil {
		return err
	}
	defer pairing.Close(dir)
	link := PairURL(host, port, code)
	pngPath := filepath.Join(dir, "pairing-qr.png")
	if png, err := qrPNG(link); err == nil && os.WriteFile(pngPath, png, 0o600) == nil {
		defer os.Remove(pngPath)
	} else {
		pngPath = ""
	}

	fmt.Fprintln(out, "Scan this with the Asksemble app (Settings > Add PC):")
	fmt.Fprintln(out)
	if err := printQR(out, link, !*light); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nPC:      https://%s:%d\n", host, port)
	fmt.Fprintf(out, "Code:    %s\n", groupCode(code))
	if pngPath != "" {
		fmt.Fprintf(out, "Image:   %s\n", pngPath)
	}
	fmt.Fprintf(out, "Expires: %s (Ctrl+C to cancel)\n\n", w.ExpiresAt.Local().Format("15:04:05"))

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "cancelled")
			return nil
		case <-tick.C:
		}
		if req, ok := pairing.PendingRequest(dir, w.ID); ok {
			return decide(ctx, dir, w.ID, req, lines, out)
		}
		if res, ok := pairing.ResultFor(dir, w.ID); ok {
			return reportResult(res, out)
		}
		if !pairing.WindowOpen(dir, w.ID) {
			return errors.New("the pairing window was closed (too many wrong codes); run pair again")
		}
		if time.Now().After(w.ExpiresAt) {
			return errors.New("the code expired; run pair again")
		}
	}
}

func decide(ctx context.Context, dir, id string, req pairing.Request, lines <-chan string, out io.Writer) error {
	fmt.Fprintf(out, "Allow %q to connect? (Tailscale account %s, address %s) [y/N]: ", req.DeviceName, req.Login, req.RemoteIP)
	var answer string
	select {
	case <-ctx.Done():
		pairing.Decide(dir, id, false)
		fmt.Fprintln(out, "\ncancelled")
		return nil
	case line, ok := <-lines:
		if !ok {
			fmt.Fprintln(out, "\n(no input)")
		}
		answer = normalizeAnswer(line)
	}
	allow := answer == "y" || answer == "yes"
	if !allow && answer != "" && answer != "n" && answer != "no" {
		fmt.Fprintf(out, "(%q is not y; refusing)\n", answer)
	}
	if err := pairing.Decide(dir, id, allow); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := pairing.ResultFor(dir, id); ok {
			return reportResult(res, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the agent did not confirm; check `asksemble-agent devices`")
}

func reportResult(res pairing.Result, out io.Writer) error {
	if res.OK {
		fmt.Fprintf(out, "Paired %s (id %s). Disconnect it any time with: asksemble-agent devices revoke %s\n", res.DeviceName, res.DeviceID, res.DeviceID)
		return nil
	}
	switch res.Reason {
	case "denied":
		fmt.Fprintln(out, "Refused. The phone was not paired.")
		return nil
	case "timeout":
		return errors.New("no answer in time; run pair again")
	}
	return fmt.Errorf("pairing failed (%s)", res.Reason)
}

// normalizeAnswer lowercases a y/N answer. PowerShell pipes can start the
// input with a byte order mark.
func normalizeAnswer(line string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, string(rune(0xFEFF)))))
}

func groupCode(code string) string {
	var parts []string
	for len(code) > 4 {
		parts = append(parts, code[:4])
		code = code[4:]
	}
	return strings.Join(append(parts, code), "-")
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
		list, err := store.List()
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPAIRED")
		for _, d := range list {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", d.ID, d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		return tw.Flush()
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
