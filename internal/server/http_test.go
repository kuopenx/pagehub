package server

import (
	"context"
	"encoding/json"
	"fmt"
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
	r := call("create_page", map[string]any{"created_by": "test-model / high", "title": "鹈鹕 <script>alert(1)</script>", "media_type": "text/html", "html": testHTML})
	p := decodePage(r).Page
	if p.CreatedBy != "test-model / high" || p.UpdatedBy != p.CreatedBy {
		t.Fatal("create attribution missing")
	}
	for _, tool := range []string{"create_page", "update_page", "patch_page"} {
		for _, value := range []any{nil, "", "   ", "model-only", "model / ", "model / high\n", 42} {
			args := map[string]any{}
			key := "updated_by"
			switch tool {
			case "create_page":
				args = map[string]any{"title": "rejected", "media_type": "text/html", "html": testHTML}
				key = "created_by"
			case "update_page":
				args = map[string]any{"id": p.ID, "title": "rejected"}
			case "patch_page":
				args = map[string]any{"id": p.ID, "expected_revision": 1, "edits": []TextEdit{{"hello", "rejected"}}}
			}
			if value != nil {
				args[key] = value
			}
			if !call(tool, args).IsError {
				t.Fatalf("%s accepted missing/invalid attribution %v", tool, value)
			}
		}
	}
	if !call("update_page", map[string]any{"id": p.ID, "title": "rejected", "updated_by": "new-model / low", "created_by": "forged / high"}).IsError {
		t.Fatal("creator override accepted")
	}
	read := call("read_page", map[string]any{"id": p.ID})
	bRead, _ := json.Marshal(read.StructuredContent)
	var source ReadOutput
	if err := json.Unmarshal(bRead, &source); err != nil || read.IsError || source.Content != testHTML || source.Page.Revision != 1 || source.Page.CreatedBy != p.CreatedBy || source.Page.UpdatedBy != p.UpdatedBy {
		t.Fatal("read_page returned incorrect source or revision")
	}
	if !call("patch_page", map[string]any{"updated_by": "test-model / high", "id": p.ID, "edits": []TextEdit{{"hello", "patch"}}}).IsError {
		t.Fatal("patch without expected_revision accepted")
	}
	patch := call("patch_page", map[string]any{"updated_by": "test-model / high", "id": p.ID, "expected_revision": 1, "edits": []TextEdit{{"hello", "patch"}, {"patch", "hello"}}})
	if patch.IsError {
		t.Fatalf("patch failed: %v", patch.Content)
	}
	if !call("patch_page", map[string]any{"updated_by": "test-model / high", "id": p.ID, "expected_revision": 1, "edits": []TextEdit{{"hello", "stale"}}}).IsError {
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
	if call("create_page", map[string]any{"created_by": "test-model / high", "title": "invalid", "media_type": "application/pdf", "html": testHTML}).IsError != true {
		t.Fatal("wrong media type accepted")
	}
	if !call("create_page", map[string]any{"created_by": "test-model / high", "title": "invalid", "media_type": "text/html", "html": testHTML, "path": "/tmp/file"}).IsError {
		t.Fatal("extra field accepted")
	}
	if !call("update_page", map[string]any{"updated_by": "test-model / high", "id": p.ID, "html": testHTML}).IsError {
		t.Fatal("replacement without media type accepted")
	}
	updated := strings.ReplaceAll(testHTML, "hello", "changed")
	u := decodePage(call("update_page", map[string]any{"updated_by": "next-model / low", "id": p.ID, "title": "重命名", "media_type": "text/html", "html": updated})).Page
	if u.CreatedAt != p.CreatedAt || u.Path != p.Path || u.CreatedBy != p.CreatedBy || u.UpdatedBy != "next-model / low" {
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
	var listed ListOutput
	if err := json.Unmarshal(b, &listed); err != nil || len(listed.Pages) != 1 || listed.Pages[0].CreatedBy != p.CreatedBy || listed.Pages[0].UpdatedBy != u.UpdatedBy {
		t.Fatal("list attribution missing", err)
	}
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
	largePage := decodePage(call("create_page", map[string]any{"created_by": "test-model / high", "title": "large", "media_type": "text/html", "html": large})).Page
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

func TestDashboardRendering(t *testing.T) {
	s, h, _ := testServer(t)
	get := func(path string) string {
		t.Helper()
		res, err := http.Get(h.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	if body := get("/"); !strings.Contains(body, `data-count="0"`) || !strings.Contains(body, "发布第一个页面") || strings.Contains(body, `id="grid"`) {
		t.Fatal("empty dashboard missing onboarding")
	}
	p, err := s.Create(`"><img src=x onerror=alert(1)> 报表`, "<html><body>x</body></html>", `<script>alert(1)</script> / high`)
	if err != nil {
		t.Fatal(err)
	}
	body := get("/")
	for _, want := range []string{`data-count="1"`, `data-id="` + p.ID + `"`, fmt.Sprintf(`style="--h:%d"`, pageHue(p.ID)), `id="grid"`, `第 1 版`, `Created by`, `Updated by`, `&lt;script&gt;alert(1)&lt;/script&gt; / high`} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	idAt := strings.Index(body, `title="`+p.ID+`">`+p.ID+`</span>`)
	byAt := strings.Index(body, `<p class="by">`)
	if idAt < 0 || byAt <= idAt {
		t.Fatal("attribution must follow the page ID")
	}
	if strings.Contains(body, `target="_blank"`) {
		t.Fatal("dashboard cards must open pages in the same tab")
	}
	if strings.Contains(body, "<script>alert(1)</script>") || strings.Contains(body, "<img src=x") || strings.Contains(body, `data-title=""`) {
		t.Fatal("dashboard title not escaped")
	}
	// Simulate legacy metadata and a later update with an unknown creator.
	s.mu.Lock()
	legacy := s.pages[p.ID]
	legacy.CreatedBy, legacy.UpdatedBy = "", ""
	s.pages[p.ID] = legacy
	s.mu.Unlock()
	body = get("/")
	if strings.Contains(body, `<p class="by">`) || strings.Contains(body, "未记录") {
		t.Fatal("unknown attribution should be hidden")
	}
	s.mu.Lock()
	legacy.UpdatedBy = "updater-model / high"
	s.pages[p.ID] = legacy
	s.mu.Unlock()
	body = get("/")
	if strings.Contains(body, "Created by") || !strings.Contains(body, "Updated by") || !strings.Contains(body, "updater-model / high") {
		t.Fatal("show only recorded attribution")
	}
	if body := get("/?q=nomatch"); !strings.Contains(body, `id="grid"`) || strings.Contains(body, `id="noresult" hidden`) || strings.Contains(body, p.ID) {
		t.Fatal("filtered dashboard should render an empty grid with a visible no-result state")
	}
}

func TestDashboardHelpers(t *testing.T) {
	id := "0f8fad5b-d9cb-469f-a165-70867728950e"
	if pageHue(id) != pageHue(id) || pageHue(id) >= 360 {
		t.Fatal("pageHue must be stable and within 0-359")
	}
}
