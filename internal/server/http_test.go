package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type tokenTransport struct{ token string }

func (tr tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+tr.token)
	return http.DefaultTransport.RoundTrip(r)
}

func testServer(t *testing.T) (*Store, *httptest.Server, int) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	h := httptest.NewUnstartedServer(NewApp(s, "test-token", port))
	h.Listener.Close()
	h.Listener = l
	h.Start()
	t.Cleanup(h.Close)
	return s, h, port
}

func TestMCPAndHTTPIntegration(t *testing.T) {
	s, h, _ := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "pagehub-test", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: h.URL + "/_mcp", HTTPClient: &http.Client{Transport: tokenTransport{"test-token"}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 6 {
		t.Fatalf("tools: %v", err)
	}
	call := func(name string, args any) *mcp.CallToolResult {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	decodePage := func(r *mcp.CallToolResult) PageOutput {
		t.Helper()
		if r.IsError {
			t.Fatalf("tool failed: %v", r.Content)
		}
		b, _ := json.Marshal(r.StructuredContent)
		var o PageOutput
		if err := json.Unmarshal(b, &o); err != nil {
			t.Fatal(err)
		}
		return o
	}
	get := func(path string) (int, string, string) {
		t.Helper()
		r, err := http.Get(h.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b), r.Header.Get("ETag")
	}
	r := call("create_page", map[string]any{"title": "鹈鹕 <script>alert(1)</script>", "media_type": "text/html", "html": testHTML})
	p := decodePage(r).Page
	read := call("read_page", map[string]any{"id": p.ID})
	bRead, _ := json.Marshal(read.StructuredContent)
	var source ReadOutput
	if err := json.Unmarshal(bRead, &source); err != nil || read.IsError || source.Content != testHTML || source.Page.Revision != 1 {
		t.Fatal("read_page returned incorrect source or revision")
	}
	if !call("patch_page", map[string]any{"id": p.ID, "edits": []TextEdit{{"hello", "patch"}}}).IsError {
		t.Fatal("patch without expected_revision accepted")
	}
	patch := call("patch_page", map[string]any{"id": p.ID, "expected_revision": 1, "edits": []TextEdit{{"hello", "patch"}, {"patch", "hello"}}})
	if patch.IsError {
		t.Fatalf("patch failed: %v", patch.Content)
	}
	if !call("patch_page", map[string]any{"id": p.ID, "expected_revision": 1, "edits": []TextEdit{{"hello", "stale"}}}).IsError {
		t.Fatal("MCP patch accepted stale revision")
	}
	if !call("read_page", map[string]any{"id": p.ID, "start_line": 0}).IsError {
		t.Fatal("schema accepted invalid read range")
	}
	status, body, etag := get(p.Path)
	if status != 200 || body != testHTML {
		t.Fatal("page not served immediately")
	}
	status, body, _ = get("/")
	if status != 200 || !strings.Contains(body, p.ID) || strings.Contains(body, "<h2>鹈鹕 <script>") {
		t.Fatal("dashboard missing page or unescaped title")
	}
	if call("create_page", map[string]any{"title": "invalid", "media_type": "application/pdf", "html": testHTML}).IsError != true {
		t.Fatal("wrong media type accepted")
	}
	if !call("create_page", map[string]any{"title": "invalid", "media_type": "text/html", "html": testHTML, "path": "/tmp/file"}).IsError {
		t.Fatal("extra field accepted")
	}
	if !call("update_page", map[string]any{"id": p.ID, "html": testHTML}).IsError {
		t.Fatal("replacement without media type accepted")
	}
	updated := strings.ReplaceAll(testHTML, "hello", "changed")
	u := decodePage(call("update_page", map[string]any{"id": p.ID, "title": "重命名", "media_type": "text/html", "html": updated})).Page
	if u.CreatedAt != p.CreatedAt || u.Path != p.Path {
		t.Fatal("update changed identity")
	}
	_, body, newETag := get(p.Path)
	if body != updated || newETag == etag {
		t.Fatal("stale page or ETag")
	}
	list := call("list_pages", map[string]any{"query": "重命名"})
	if list.IsError {
		t.Fatal("search failed")
	}
	b, _ := json.Marshal(list.StructuredContent)
	if !strings.Contains(string(b), p.ID) {
		t.Fatal("search missing page")
	}
	if call("delete_page", map[string]any{"id": p.ID}).IsError {
		t.Fatal("delete failed")
	}
	status, _, _ = get(p.Path)
	if status != 404 || s.Count() != 0 {
		t.Fatal("deleted page remains")
	}
	if _, err := os.Stat(filepath.Join(s.dir, p.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted directory remains")
	}
	// SDK has a default 4 MiB request limit. Verify we honor the user's no-size-limit policy.
	large := "<html><body>" + strings.Repeat("x", 5<<20) + "</body></html>"
	largePage := decodePage(call("create_page", map[string]any{"title": "large", "media_type": "text/html", "html": large})).Page
	if largePage.SizeBytes != int64(len(large)) {
		t.Fatal("large HTML truncated")
	}
}

func TestManagementIsolation(t *testing.T) {
	_, h, port := testServer(t)
	for _, tc := range []struct {
		token, origin, host string
		status              int
	}{
		{"", "", "", 401}, {"test-token", "http://evil.example", "", 403}, {"test-token", "", "evil.example", 403},
	} {
		r, _ := http.NewRequest("POST", h.URL+"/_mcp", strings.NewReader(`{}`))
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer "+tc.token)
		}
		r.Header.Set("Origin", tc.origin)
		if tc.host != "" {
			r.Host = tc.host
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("status %d want %d", resp.StatusCode, tc.status)
		}
	}
	request := httptest.NewRequest("POST", "http://127.0.0.1:8765/_mcp", nil)
	request.RemoteAddr = "192.168.31.99:1234"
	request.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	managementOnly("test-token", port, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("LAN reached MCP handler") })).ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatal("LAN management allowed")
	}
}
