// Integration acceptance tool. Mutates only its own temporary pages and service.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/kuopenx/pagehub/internal/localhttp"
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func ensure(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func main() {
	binary := flag.String("binary", "./pagehub", "CLI binary to validate")
	endpoint := flag.String("endpoint", "", "Existing loopback MCP endpoint; creates/deletes only temporary pages")
	tokenFile := flag.String("token-file", "", "Required with --endpoint")
	output := flag.String("output", "", "Optional JSON report path")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	bin, err := filepath.Abs(*binary)
	must(err)
	checks := []string{}
	var temp string
	var cleanup func()
	base := ""
	token := ""
	run := func(args ...string) []byte {
		cmd := exec.CommandContext(ctx, bin, args...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			panic(fmt.Sprintf("%v: %v: %s", args, e, b))
		}
		return b
	}
	if *endpoint == "" {
		prefix := "pagehub-acceptance-"
		if runtime.GOOS == "linux" {
			prefix = "pagehub-acceptance space % $HOME \"-"
		}
		temp, err = os.MkdirTemp("", prefix)
		must(err)
		defer os.RemoveAll(temp)
		listener, e := net.Listen("tcp4", "127.0.0.1:0")
		must(e)
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		label := fmt.Sprintf("io.pagehub.test.%d", time.Now().UnixNano())
		common := []string{"--data-dir", temp, "--port", fmt.Sprint(port), "--service-name", label, "--json"}
		command := func(args ...string) []byte { return run(append(args, common...)...) }
		ensure(strings.Contains(string(run("--help")), "Commands:"), "help")
		run("version", "--json")
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			command("setup")
			cleanup = func() {
				cmd := exec.Command(bin, append([]string{"uninstall"}, common...)...)
				b, e := cmd.CombinedOutput()
				if e != nil {
					fmt.Fprintf(os.Stderr, "cleanup failed: %v %s\n", e, b)
				}
			}
			defer cleanup()
			command("setup")
			status := command("service", "status")
			if runtime.GOOS == "linux" {
				var report struct {
					Status struct {
						Enabled bool `json:"enabled"`
					} `json:"status"`
				}
				must(json.Unmarshal(status, &report))
				ensure(report.Status.Enabled, "Linux login autostart not enabled")
			}
			command("service", "stop")
			command("service", "stop")
			command("service", "start")
			command("service", "restart")
			checks = append(checks, "real "+runtime.GOOS+" setup twice; service status/stop twice/start/restart")
			oldLabel := label
			probe, e := net.Listen("tcp4", "127.0.0.1:0")
			must(e)
			port = probe.Addr().(*net.TCPAddr).Port
			probe.Close()
			label += ".renamed"
			common = []string{"--data-dir", temp, "--port", fmt.Sprint(port), "--service-name", label, "--json"}
			command("setup")
			home, e := os.UserHomeDir()
			must(e)
			if runtime.GOOS == "darwin" {
				_, e = exec.CommandContext(ctx, "launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), oldLabel)).CombinedOutput()
				ensure(e != nil, "old LaunchAgent remained registered after rename")
				_, e = os.Stat(filepath.Join(home, "Library", "LaunchAgents", oldLabel+".plist"))
			} else {
				out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", oldLabel+".service", "--property=LoadState", "--value").CombinedOutput()
				must(err)
				ensure(strings.TrimSpace(string(out)) == "not-found", "old systemd unit remained after rename")
				root := os.Getenv("XDG_CONFIG_HOME")
				if !filepath.IsAbs(root) {
					root = filepath.Join(home, ".config")
				}
				_, e = os.Stat(filepath.Join(root, "systemd", "user", oldLabel+".service"))
			}
			ensure(os.IsNotExist(e), "old service file remained after rename")
			command("setup")
			checks = append(checks, "real service label/port migration; old registration removed; repeated setup")
		} else {
			serveCtx, stop := context.WithCancel(context.Background())
			cmd := exec.CommandContext(serveCtx, bin, "serve", "--data-dir", temp, "--port", fmt.Sprint(port))
			must(cmd.Start())
			defer func() { stop(); _ = cmd.Wait() }()
		}
		base = fmt.Sprintf("http://127.0.0.1:%d", port)
		waitHTTP(ctx, base+"/_health")
		b, e := os.ReadFile(filepath.Join(temp, "token"))
		must(e)
		token = strings.TrimSpace(string(b))
		for _, name := range []string{"codex", "claude"} {
			file := filepath.Join(temp, name+"-config")
			if name == "codex" {
				must(os.WriteFile(file, []byte("developer_instructions = '''\nExample:\n[mcp_servers.pagehub]\nurl = 'example'\n'''\nmodel = \"preserved\"\n[mcp_servers.other]\ncommand = \"preserved\"\n"), 0600))
			} else {
				must(os.WriteFile(file, []byte(`{"number":9007199254740993,"mcpServers":{"other":{"command":"preserved"}}}`), 0600))
			}
			command("connect", name, "--config-file", file)
			command("connect", name, "--config-file", file)
			b, e := os.ReadFile(file)
			must(e)
			ensure(strings.Contains(string(b), "preserved"), "other config preserved")
			command("disconnect", name, "--config-file", file)
			command("disconnect", name, "--config-file", file)
			b, e = os.ReadFile(file)
			must(e)
			ensure(!strings.Contains(string(b), token), "token removed on disconnect")
		}
		checks = append(checks, "real connect/disconnect twice for both clients; unrelated configuration preserved")
		command("doctor")
		command("open")
		checks = append(checks, "real doctor and open")
	} else {
		ensure(strings.HasPrefix(*endpoint, "http://127.0.0.1:"), "acceptance endpoint must be IPv4 loopback")
		ensure(*tokenFile != "", "token-file required")
		base = strings.TrimSuffix(*endpoint, "/_mcp")
		b, e := os.ReadFile(*tokenFile)
		must(e)
		token = strings.TrimSpace(string(b))
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "pagehub-acceptance", Version: "0.3.0"}, nil)
	mcpHTTP, err := localhttp.NewClient(base+"/_mcp", token, 15*time.Second)
	must(err)
	defer mcpHTTP.CloseIdleConnections()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: base + "/_mcp", HTTPClient: mcpHTTP, DisableStandaloneSSE: true}, nil)
	must(err)
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	must(err)
	ensure(len(tools.Tools) == 6, "six tools")
	call := func(name string, args any, fail bool) map[string]any {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		must(e)
		if fail {
			ensure(r.IsError, name+" should fail")
			return nil
		}
		ensure(!r.IsError, fmt.Sprintf("%s: %v", name, r.Content))
		b, e := json.Marshal(r.StructuredContent)
		must(e)
		var out map[string]any
		must(json.Unmarshal(b, &out))
		return out
	}
	before := call("list_pages", map[string]any{"limit": 0}, false)
	existing := map[string][32]byte{}
	for _, v := range before["pages"].([]any) {
		p := v.(map[string]any)
		id := p["id"].(string)
		r := call("read_page", map[string]any{"id": id}, false)
		existing[id] = sha256.Sum256([]byte(r["content"].(string)))
	}
	html := "<!doctype html>\n<html lang=\"zh-CN\">\n<meta charset=\"UTF-8\">\n<title>Pagehub acceptance</title>\n<svg viewBox=\"0 0 400 200\"><circle cx=\"70\" cy=\"120\" r=\"30\" fill=\"#25756b\"/><text x=\"120\" y=\"130\">MCP live test</text></svg>\n</html>\n"
	p := call("create_page", map[string]any{"created_by": "test-model / high", "title": "Pagehub 临时验收", "media_type": "text/html", "html": html}, false)["page"].(map[string]any)
	ensure(p["created_by"] == "test-model / high" && p["updated_by"] == p["created_by"], "creation attribution")
	id := p["id"].(string)
	call("create_page", map[string]any{"title": "rejected", "media_type": "text/html", "html": html}, true)
	call("update_page", map[string]any{"id": id, "title": "rejected"}, true)
	call("patch_page", map[string]any{"id": id, "expected_revision": 1, "edits": []map[string]string{{"old_text": "MCP live test", "new_text": "rejected"}}}, true)
	deleted := false
	defer func() {
		if !deleted {
			_, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_page", Arguments: map[string]any{"id": id}})
		}
	}()
	read := call("read_page", map[string]any{"id": id}, false)
	ensure(read["content"] == html, "full read")
	part := call("read_page", map[string]any{"id": id, "start_line": 2, "end_line": 3}, false)
	ensure(part["content"] == "<html lang=\"zh-CN\">\n<meta charset=\"UTF-8\">\n", "line read")
	call("patch_page", map[string]any{"updated_by": "test-model / high", "id": id, "expected_revision": 1, "edits": []map[string]string{{"old_text": "MCP live test", "new_text": "changed"}, {"old_text": "__missing_anchor__", "new_text": "bad"}}}, true)
	ensure(call("read_page", map[string]any{"id": id}, false)["content"] == html, "atomic rejection")
	patch := call("patch_page", map[string]any{"updated_by": "patch-model / medium", "id": id, "expected_revision": 1, "edits": []map[string]string{{"old_text": "MCP live test", "new_text": "Go CLI + MCP verified"}}}, false)
	ensure(patch["page"].(map[string]any)["updated_by"] == "patch-model / medium", "patch attribution")
	ensure(patch["page"].(map[string]any)["revision"].(float64) == 2, "revision increment")
	call("patch_page", map[string]any{"updated_by": "test-model / high", "id": id, "expected_revision": 1, "edits": []map[string]string{{"old_text": "Go CLI + MCP verified", "new_text": "stale"}}}, true)
	call("update_page", map[string]any{"updated_by": "update-model / low", "id": id, "title": "Pagehub verified", "expected_revision": 2}, false)
	final := call("read_page", map[string]any{"id": id}, false)
	ensure(final["page"].(map[string]any)["created_by"] == p["created_by"] && final["page"].(map[string]any)["updated_by"] == "update-model / low", "creator preserved and updater recorded")
	ensure(final["page"].(map[string]any)["revision"].(float64) == 3, "update revision")
	ensure(final["page"].(map[string]any)["created_at"] == p["created_at"], "creation time preserved")
	r, err := http.Get(base + p["path"].(string))
	must(err)
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	must(err)
	ensure(string(b) == final["content"], "HTTP matches MCP")
	call("delete_page", map[string]any{"id": id}, false)
	deleted = true
	r, err = http.Get(base + p["path"].(string))
	must(err)
	r.Body.Close()
	ensure(r.StatusCode == 404, "delete 404")
	after := call("list_pages", map[string]any{"limit": 0}, false)
	ensure(after["total"] == before["total"], "page count preserved")
	for id, hash := range existing {
		r := call("read_page", map[string]any{"id": id}, false)
		ensure(sha256.Sum256([]byte(r["content"].(string))) == hash, "existing page preserved")
	}
	checks = append(checks, "all six tools over real MCP HTTP", "required model attribution; immutable creator and latest updater", "source and exact line ranges", "failed batch saves nothing", "stale revision rejected", "URL and creation time preserved", "HTTP serves patched source", "temporary page deleted; existing pages unchanged")
	if temp != "" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
		run("uninstall", "--data-dir", temp, "--service-name", func() string {
			var s struct {
				ServiceName string `json:"service_name"`
			}
			b, _ := os.ReadFile(filepath.Join(temp, "settings.json"))
			_ = json.Unmarshal(b, &s)
			return s.ServiceName
		}(), "--json")
		ensure(fileExists(filepath.Join(temp, "token")), "uninstall keeps token")
		checks = append(checks, "real uninstall; persistent data retained")
	}
	report := map[string]any{"verified_at": time.Now().UTC().Format(time.RFC3339), "checks": checks}
	b, err = json.MarshalIndent(report, "", "  ")
	must(err)
	if *output != "" {
		must(os.WriteFile(*output, append(b, '\n'), 0600))
	}
	fmt.Println(string(b))
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func waitHTTP(ctx context.Context, url string) {
	for range 100 {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		r, e := http.DefaultClient.Do(req)
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return
			}
		}
		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	panic("HTTP startup timeout")
}
