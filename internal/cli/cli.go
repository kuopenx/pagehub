package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/kuopenx/pagehub/internal/buildinfo"
	"github.com/kuopenx/pagehub/internal/clients"
	"github.com/kuopenx/pagehub/internal/config"
	"github.com/kuopenx/pagehub/internal/server"
	"github.com/kuopenx/pagehub/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Environment struct {
	Home, Executable, GOOS string
	UID                    int
	Runner                 service.Runner
	Serve                  func(context.Context, string, int) error
	Ready                  func(context.Context) error
	Open                   func(context.Context, string) error
}
type options struct {
	dir, clientFile, label string
	port                   int
	json                   bool
}
type result struct {
	Command      string          `json:"command"`
	Version      string          `json:"version,omitempty"`
	Message      string          `json:"message,omitempty"`
	DashboardURL string          `json:"dashboard_url,omitempty"`
	LANURLs      []string        `json:"lan_urls,omitempty"`
	Status       *service.Status `json:"status,omitempty"`
	Checks       []Check         `json:"checks,omitempty"`
}
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

const help = `Pagehub — single-file HTML artifacts on your LAN
Usage: pagehub <command> [options]

Commands:
  serve                         Run HTTP + MCP in the foreground
  setup                         Install/update and start a macOS user service
  service start|stop|restart|status
  connect codex|claude           Register this service in a client configuration
  disconnect codex|claude        Remove only this endpoint's registration
  doctor                        Check service, HTTP, MCP and client registrations
  open                          Open the dashboard (always prints its URL)
  version                       Print version and build information
  uninstall                     Stop/remove service and managed binary; preserve data

Options:
  --data-dir DIR                Default: ~/.pagehub
  --port PORT                   Override saved/default port (8765)
  --service-name NAME           Override saved/default launchd label
  --config-file FILE            connect/disconnect: override client configuration path
  --json                        Structured output; errors on stderr; nonzero on failure
  --help                        Show help

Background management supports macOS. Foreground serve is portable.
setup is repeatable. Updates preserve pages, token and revisions.
uninstall preserves pages, settings, token and client configuration;
use disconnect first if you want to remove client registrations.
`

func Run(ctx context.Context, args []string, out, errOut io.Writer, env Environment) int {
	if env.Home == "" {
		var err error
		env.Home, err = os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}
	if env.UID == 0 {
		env.UID = os.Getuid()
	}
	if env.Serve == nil {
		env.Serve = server.Serve
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, help)
		return 0
	}
	command := args[0]
	rest := args[1:]
	role := ""
	if strings.HasPrefix(command, "--") {
		command = "serve"
		rest = args
	} // Legacy 0.2 LaunchAgent flags.
	if command == "service" || command == "connect" || command == "disconnect" {
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			role = rest[0]
			rest = rest[1:]
		}
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	o := options{}
	fs.StringVar(&o.dir, "data-dir", filepath.Join(env.Home, ".pagehub"), "Private data directory")
	fs.IntVar(&o.port, "port", 0, "Port override")
	fs.StringVar(&o.label, "service-name", "", "Service name override")
	fs.StringVar(&o.clientFile, "config-file", "", "Client configuration override")
	fs.BoolVar(&o.json, "json", false, "JSON output")
	fs.Usage = func() { fmt.Fprint(out, help) }
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected arguments")
		return 2
	}
	allowed := map[string]bool{"serve": true, "setup": true, "service": true, "connect": true, "disconnect": true, "doctor": true, "open": true, "version": true, "uninstall": true}
	if !allowed[command] {
		fmt.Fprintln(errOut, "unknown command:", command)
		return 2
	}
	if o.clientFile != "" && command != "connect" && command != "disconnect" {
		fmt.Fprintln(errOut, "--config-file is only valid for connect/disconnect")
		return 2
	}
	var err error
	o.dir, err = filepath.Abs(o.dir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	settings, err := config.Load(o.dir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	portSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portSet = true
		}
	})
	if portSet {
		settings.Port = o.port
	}
	if o.label != "" {
		settings.ServiceName = o.label
	}
	if err = settings.Validate(); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	m := service.Manager{Home: env.Home, DataDir: o.dir, GOOS: env.GOOS, UID: env.UID, Settings: settings, Run: env.Runner, Ready: env.Ready}
	r := result{Command: command}
	url := fmt.Sprintf("http://127.0.0.1:%d/", settings.Port)
	commandCtx := ctx
	cancel := func() {}
	if command != "serve" {
		commandCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	}
	defer cancel()
	ready := func() error {
		if env.Ready != nil {
			return env.Ready(commandCtx)
		}
		return m.Wait(commandCtx)
	}
	switch command {
	case "version":
		r.Version = buildinfo.Version
		r.Message = "commit=" + buildinfo.Commit + " date=" + buildinfo.Date
	case "serve":
		err = env.Serve(ctx, o.dir, settings.Port)
		r.Message = "server stopped"
	case "setup":
		if env.GOOS != "darwin" {
			err = errors.New("setup currently supports macOS only; use pagehub serve")
			break
		}
		source := env.Executable
		if source == "" {
			source, err = os.Executable()
			if err != nil {
				break
			}
		}
		if err = os.MkdirAll(o.dir, 0700); err != nil {
			break
		}
		if _, err = server.LoadToken(filepath.Join(o.dir, "token")); err != nil {
			break
		}
		if err = m.Install(commandCtx, source); err != nil {
			break
		}
		r.Message = "installed and started; existing pages and token preserved"
		r.DashboardURL = url
		r.LANURLs = lanURLs(settings.Port)
	case "service":
		switch role {
		case "status":
			var s service.Status
			s, err = m.Status(commandCtx)
			r.Status = &s
		case "start":
			err = m.Start(commandCtx)
			if err == nil {
				err = ready()
			}
		case "stop":
			err = m.Stop(commandCtx)
		case "restart":
			err = m.Restart(commandCtx)
			if err == nil {
				err = ready()
			}
		default:
			err = errors.New("service requires start, stop, restart or status")
		}
		r.Message = "service " + role
	case "connect", "disconnect":
		path := o.clientFile
		if path == "" {
			path, err = clients.DefaultPath(env.Home, role)
			if err != nil {
				break
			}
		}
		token := ""
		if command == "connect" {
			var b []byte
			b, err = os.ReadFile(filepath.Join(o.dir, "token"))
			if err != nil {
				err = errors.New("management token missing; run setup or serve first")
				break
			}
			token = strings.TrimSpace(string(b))
		}
		err = (clients.Client{Name: role, Path: path, URL: url + "_mcp", Token: token}).Change(command == "connect")
		r.Message = command + " " + role + " completed; other registrations preserved"
	case "uninstall":
		err = m.Uninstall(commandCtx)
		r.Message = "service removed; pages, token, settings and client configuration preserved"
	case "open":
		r.DashboardURL = url
		r.LANURLs = lanURLs(settings.Port)
		if env.Open != nil {
			err = env.Open(commandCtx, url)
		} else {
			if env.GOOS == "darwin" {
				err = exec.CommandContext(commandCtx, "open", url).Run()
			} else if env.GOOS == "linux" {
				err = exec.CommandContext(commandCtx, "xdg-open", url).Run()
			} else {
				err = errors.New("browser launch unsupported on this platform")
			}
		}
		if err != nil {
			r.Message = "could not launch browser; open dashboard_url manually"
			err = nil
		}
	case "doctor":
		r.DashboardURL = url
		r.LANURLs = lanURLs(settings.Port)
		r.Checks = diagnose(commandCtx, m, env.Home, url)
		for _, check := range r.Checks {
			if !check.OK {
				err = errors.New("doctor found problems; see checks")
			}
		}
	}
	if o.json {
		_ = json.NewEncoder(out).Encode(r)
	} else {
		if r.Version != "" {
			fmt.Fprintln(out, "pagehub", r.Version)
		}
		if r.Message != "" {
			fmt.Fprintln(out, r.Message)
		}
		if r.DashboardURL != "" {
			fmt.Fprintln(out, "Dashboard:", r.DashboardURL)
		}
		for _, u := range r.LANURLs {
			fmt.Fprintln(out, "LAN:", u)
		}
		if r.Status != nil {
			fmt.Fprintf(out, "registered=%t running=%t pid=%d label=%s\n", r.Status.Registered, r.Status.Running, r.Status.PID, r.Status.Label)
		}
		for _, c := range r.Checks {
			fmt.Fprintf(out, "%s: ok=%t %s\n", c.Name, c.OK, c.Detail)
		}
	}
	if err != nil {
		if o.json {
			_ = json.NewEncoder(errOut).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(errOut, "pagehub:", err)
		}
		return 1
	}
	return 0
}
func lanURLs(port int) []string {
	out := []string{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, e := net.ParseCIDR(addr.String())
			if e == nil && ip.To4() != nil && ip.IsPrivate() {
				out = append(out, fmt.Sprintf("http://%s:%d/", ip, port))
			}
		}
	}
	return out
}

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}
func diagnose(ctx context.Context, m service.Manager, home, url string) []Check {
	checks := []Check{}
	if m.GOOS == "darwin" {
		s, err := m.Status(ctx)
		checks = append(checks, Check{"service", err == nil && s.Running, fmt.Sprintf("registered=%t running=%t", s.Registered, s.Running)})
	}
	hc := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"_health", nil)
	resp, err := hc.Do(req)
	healthOK := false
	detail := "HTTP not reachable; check port and pagehub.log"
	if err == nil {
		defer resp.Body.Close()
		var h struct{ Status, Version string }
		err = json.NewDecoder(resp.Body).Decode(&h)
		healthOK = err == nil && resp.StatusCode == 200 && h.Status == "ok"
		detail = "version=" + h.Version
	}
	checks = append(checks, Check{"http", healthOK, detail})
	b, err := os.ReadFile(filepath.Join(m.DataDir, "token"))
	if err != nil {
		checks = append(checks, Check{"mcp", false, "token missing"})
		return checks
	}
	token := strings.TrimSpace(string(b))
	client := mcp.NewClient(&mcp.Implementation{Name: "pagehub-doctor", Version: buildinfo.Version}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url + "_mcp", HTTPClient: &http.Client{Transport: authTransport{token}, Timeout: 5 * time.Second}, DisableStandaloneSSE: true}, nil)
	mcpOK := false
	if err == nil {
		defer session.Close()
		tools, e := session.ListTools(ctx, nil)
		mcpOK = e == nil && len(tools.Tools) == 6
	}
	checks = append(checks, Check{"mcp", mcpOK, "expected six authenticated tools"})
	for _, name := range []string{"codex", "claude"} {
		path, _ := clients.DefaultPath(home, name)
		c := clients.Client{Name: name, Path: path, URL: url + "_mcp", Token: token}
		connected, e := c.Connected()
		detail := "not configured (optional); use pagehub connect " + name
		if connected {
			detail = "configured for this endpoint"
		}
		if errors.Is(e, clients.ErrForeignEndpoint) {
			detail = "configured for another Pagehub instance (optional)"
			e = nil
		}
		checks = append(checks, Check{name, e == nil, detail})
	}
	if len(lanURLs(m.Settings.Port)) == 0 {
		checks = append(checks, Check{"lan", true, "no private IPv4 interface; local access remains available"})
	} else {
		checks = append(checks, Check{"lan", true, "LAN URLs detected; phone access also depends on firewall and Wi-Fi"})
	}
	if m.GOOS == "darwin" {
		runner := m.Run
		if runner == nil {
			runner = service.Exec
		}
		state, e := runner(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
		detail := "firewall status unavailable; check macOS System Settings if LAN access fails"
		if e == nil {
			detail = strings.TrimSpace(state)
			if strings.Contains(strings.ToLower(state), "enabled") {
				rule, ruleErr := runner(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getappblocked", m.Binary())
				if ruleErr == nil {
					detail += "; " + strings.TrimSpace(rule)
				} else {
					detail += "; Pagehub rule not readable: allow incoming connections in System Settings"
				}
			}
		}
		checks = append(checks, Check{"firewall", true, detail}) // Advisory; reachability is verified separately from a LAN client.
	}

	return checks
}
