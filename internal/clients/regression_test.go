package clients

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexMultilineExamplesPreservedAcrossLifecycle(t *testing.T) {
	for _, delimiter := range []string{"'''", `"""`} {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(delimiter+newline, func(t *testing.T) {
				source := "# keep example\ndeveloper_instructions = " + delimiter + "\nExample:\n[mcp_servers.pagehub]\nurl = 'example'\n[mcp_servers.pagehub.http_headers]\nAuthorization = 'example'\n[[examples]]\n" + delimiter + "\nmodel = 'test'\n[mcp_servers.other]\ncommand = 'other'\n"
				source = strings.ReplaceAll(source, "\n", newline)
				path := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(path, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				c := Client{Name: "codex", Path: path, URL: "http://127.0.0.1:8765/_mcp", Token: "test-token"}
				if err := c.Change(true); err != nil {
					t.Fatal(err)
				}
				first, _ := os.ReadFile(path)
				if !bytes.Contains(first, []byte(strings.TrimRight(source, "\r\n"))) {
					t.Fatal("example bytes changed")
				}
				if err := c.Change(true); err != nil {
					t.Fatal(err)
				}
				second, _ := os.ReadFile(path)
				if !bytes.Equal(first, second) {
					t.Fatal("repeat connect not byte stable")
				}
				for range 2 {
					if err := c.Change(false); err != nil {
						t.Fatal(err)
					}
				}
				final, _ := os.ReadFile(path)
				if strings.TrimRight(string(final), "\r\n") != strings.TrimRight(source, "\r\n") {
					t.Fatal("unrelated content changed")
				}
			})
		}
	}
}

func TestCodexParsedEscapedKeysAndNestedArrayTables(t *testing.T) {
	source := "model='test'\n[\"mcp_servers\".\"pagehu\\u0062\"]\nurl='http://127.0.0.1:8765/_mcp'\n[[mcp_servers.pagehub.examples]]\nname='first'\n[mcp_servers.other]\ncommand='other'\n"
	c := Client{URL: "http://127.0.0.1:8765/_mcp", Token: "test-token"}
	result, err := c.codex([]byte(source), false)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "model='test'\n[mcp_servers.other]\ncommand='other'\n" {
		t.Fatalf("unexpected preserved config: %s", result)
	}
}
