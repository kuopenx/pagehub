package clients

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientLifecyclePreservesUnrelatedSettings(t *testing.T) {
	for _, name := range []string{"codex", "claude"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			source := `{"theme":"dark","number":9007199254740993,"mcpServers":{"other":{"command":"other"}}}`
			if name == "codex" {
				source = "# keep comments\nmodel = \"test\"\n[mcp_servers.other]\ncommand = \"other\"\n"
			}
			if err := os.WriteFile(path, []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
			c := Client{Name: name, Path: path, URL: "http://127.0.0.1:8765/_mcp", Token: "test-token"}
			for range 2 {
				if err := c.Change(true); err != nil {
					t.Fatal(err)
				}
			}
			ok, err := c.Connected()
			if err != nil || !ok {
				t.Fatal("not connected", err)
			}
			b, _ := os.ReadFile(path)
			if !strings.Contains(string(b), "other") {
				t.Fatal("other registration removed")
			}
			if name == "codex" && !strings.Contains(string(b), "# keep comments") {
				t.Fatal("comments removed")
			}
			if name == "claude" && !strings.Contains(string(b), "9007199254740993") {
				t.Fatal("number rounded")
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0600 {
				t.Fatal("insecure permissions")
			}
			for range 2 {
				if err := c.Change(false); err != nil {
					t.Fatal(err)
				}
			}
			ok, err = c.Connected()
			if err != nil || ok {
				t.Fatal("not disconnected")
			}
			b, _ = os.ReadFile(path)
			if strings.Contains(string(b), "test-token") {
				t.Fatal("token remains")
			}
			if !strings.Contains(string(b), "other") {
				t.Fatal("other registration removed on disconnect")
			}
		})
	}
}
func TestRejectForeignAndMalformedConfig(t *testing.T) {
	for _, tc := range []struct{ name, source string }{{"codex", "[mcp_servers.pagehub]\nurl=\"http://different\""}, {"claude", `{"mcpServers":{"pagehub":{"url":"http://different"}}}`}, {"codex", "invalid!"}, {"claude", "{"}} {
		path := filepath.Join(t.TempDir(), "config")
		_ = os.WriteFile(path, []byte(tc.source), 0600)
		c := Client{Name: tc.name, Path: path, URL: "http://127.0.0.1:8765/_mcp", Token: "secret"}
		for _, connect := range []bool{true, false} {
			if err := c.Change(connect); err == nil {
				t.Fatal("invalid/foreign config accepted")
			}
			b, _ := os.ReadFile(path)
			if string(b) != tc.source {
				t.Fatal("failed command changed config")
			}
		}
	}
}
func TestCodexNestedHeadersAndQuotedName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(path, []byte("[mcp_servers.\"pagehub\"]\nurl=\"http://127.0.0.1:8765/_mcp\"\nenabled=true\n[mcp_servers.\"pagehub\".http_headers]\nAuthorization=\"old\"\n[mcp_servers.other]\nurl=\"http://other\"\n"), 0600)
	c := Client{Name: "codex", Path: path, URL: "http://127.0.0.1:8765/_mcp", Token: "new"}
	if err := c.Change(true); err != nil {
		t.Fatal(err)
	}
	ok, err := c.Connected()
	if !ok || err != nil {
		t.Fatal(err)
	}
}
