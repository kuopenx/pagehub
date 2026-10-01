// Live acceptance test and initial page import. Every mutation uses MCP.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func main() {
	home, err := os.UserHomeDir()
	check(err)
	endpoint := flag.String("endpoint", "http://127.0.0.1:8765/_mcp", "MCP endpoint")
	file := flag.String("html-file", "", "Pelican HTML to import")
	tokenFile := flag.String("token-file", filepath.Join(home, ".pagehub", "token"), "Management token file")
	output := flag.String("output", "verification.json", "Acceptance report")
	flag.Parse()
	if *file == "" {
		panic("--html-file is required")
	}
	token, err := os.ReadFile(*tokenFile)
	check(err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "pagehub-acceptance", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: *endpoint, HTTPClient: &http.Client{Transport: authTransport{strings.TrimSpace(string(token))}}, DisableStandaloneSSE: true}, nil)
	check(err)
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	check(err)
	ensure(len(tools.Tools) == 6, "six MCP tools")
	call := func(name string, args any) (map[string]any, bool) {
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		check(err)
		if r.IsError {
			return nil, true
		}
		b, err := json.Marshal(r.StructuredContent)
		check(err)
		var out map[string]any
		check(json.Unmarshal(b, &out))
		return out, false
	}
	okCall := func(name string, args any) map[string]any {
		r, bad := call(name, args)
		ensure(!bad, name+" succeeds")
		return r
	}
	base := strings.TrimSuffix(*endpoint, "/_mcp")
	get := func(path string) (int, string) {
		r, err := http.Get(base + path)
		check(err)
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		check(err)
		return r.StatusCode, string(b)
	}
	checks := []string{"official SDK connection", "six MCP tools"}
	_, bad := call("create_page", map[string]any{"title": "invalid", "media_type": "application/pdf", "html": "<html></html>"})
	ensure(bad, "wrong resource type rejected")
	_, bad = call("create_page", map[string]any{"title": "invalid", "media_type": "text/html", "html": "<html></html>", "path": "/tmp/file"})
	ensure(bad, "file path rejected")
	checks = append(checks, "resource type and extra-field validation")
	ids := []string{}
	defer func() {
		for _, id := range ids {
			_, _ = call("delete_page", map[string]any{"id": id})
		}
	}()
	var first map[string]any
	for i := 0; i < 2; i++ {
		out := okCall("create_page", map[string]any{"title": "验收临时页面", "media_type": "text/html", "html": "<!doctype html><html><body>original</body></html>"})
		page := out["page"].(map[string]any)
		ids = append(ids, page["id"].(string))
		if i == 0 {
			first = page
		}
	}
	ensure(ids[0] != ids[1], "duplicate titles have unique IDs")
	status, body := get("/")
	ensure(status == 200 && strings.Contains(body, ids[0]) && strings.Contains(body, ids[1]), "dashboard live additions")
	checks = append(checks, "same-name creation with distinct UUIDs", "dashboard reflects additions")
	updated := okCall("update_page", map[string]any{"id": ids[0], "title": "验收更新页面", "media_type": "text/html", "html": "<!doctype html><html><body>updated</body></html>"})["page"].(map[string]any)
	ensure(updated["created_at"] == first["created_at"] && updated["path"] == first["path"], "update preserves identity")
	status, body = get(updated["path"].(string))
	ensure(status == 200 && strings.Contains(body, ">updated<"), "live updated content")
	listed := okCall("list_pages", map[string]any{"query": "验收更新页面"})
	ensure(listed["total"].(float64) == 1, "MCP search")
	checks = append(checks, "update preserves URL and creation time", "updated HTML served immediately", "MCP search")
	for _, id := range ids {
		okCall("delete_page", map[string]any{"id": id})
		status, _ = get("/" + id + "/")
		ensure(status == 404, "deleted URL returns 404")
		_, err := os.Stat(filepath.Join(home, ".pagehub", "pages", id))
		ensure(os.IsNotExist(err), "deleted files gone")
	}
	ids = nil
	status, body = get("/")
	ensure(status == 200 && !strings.Contains(body, first["id"].(string)), "dashboard removes deleted page")
	checks = append(checks, "deleted URLs return 404", "deleted files and index removed", "dashboard reflects deletions")
	html, err := os.ReadFile(*file)
	check(err)
	pelican := okCall("create_page", map[string]any{"title": "鹈鹕骑自行车", "media_type": "text/html", "html": string(html)})["page"].(map[string]any)
	status, body = get(pelican["path"].(string))
	ensure(status == 200 && body == string(html), "pelican content matches original")
	checks = append(checks, "pelican imported via MCP", "served HTML equals original")
	report := map[string]any{"verified_at": time.Now().UTC().Format(time.RFC3339), "checks": checks, "pelican": pelican, "dashboard": base + "/", "mcp_endpoint": *endpoint}
	b, err := json.MarshalIndent(report, "", "  ")
	check(err)
	check(os.WriteFile(*output, append(b, '\n'), 0600))
	fmt.Println(string(b))
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
func ensure(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
