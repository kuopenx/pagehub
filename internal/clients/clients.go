package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kuopenx/pagehub/internal/config"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

var ErrForeignEndpoint = errors.New("pagehub registration points to a different endpoint")

type Client struct{ Name, Path, URL, Token string }

func DefaultPath(home, name string) (string, error) {
	switch name {
	case "codex":
		return filepath.Join(home, ".codex", "config.toml"), nil
	case "claude":
		return filepath.Join(home, ".claude.json"), nil
	default:
		return "", errors.New("client must be codex or claude")
	}
}
func object(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("invalid client configuration object")
	}
	return m, nil
}
func owned(entry any, url string) error {
	if entry == nil {
		return nil
	}
	m, err := object(entry)
	if err != nil {
		return err
	}
	if m["url"] != url {
		return fmt.Errorf("%w; left unchanged", ErrForeignEndpoint)
	}
	if t, ok := m["type"]; ok && t != "http" {
		return errors.New("MCP named pagehub is not an HTTP server; left unchanged")
	}
	return nil
}
func (c Client) Change(connect bool) error {
	original, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		original = nil
	} else if err != nil {
		return err
	}
	var next []byte
	switch c.Name {
	case "codex":
		next, err = c.codex(original, connect)
	case "claude":
		next, err = c.claude(original, connect)
	default:
		return errors.New("client must be codex or claude")
	}
	if err != nil {
		return err
	}
	current, err := os.ReadFile(c.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !bytes.Equal(current, original) {
		return errors.New("client configuration changed concurrently; retry")
	}
	if len(next) == 0 && original == nil {
		return nil
	}
	if bytes.Equal(next, original) {
		return os.Chmod(c.Path, 0600)
	}
	return config.AtomicWrite(c.Path, next, 0600)
}

// Use parsed table keys and byte offsets, so examples inside multiline
// strings cannot be mistaken for configuration. Retain unrelated bytes.
func withoutPagehubTables(original []byte) ([]byte, bool, error) {
	var parser unstable.Parser
	parser.Reset(original)
	var out bytes.Buffer
	start, skip, found := 0, false, false
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.Table && node.Kind != unstable.ArrayTable {
			continue
		}
		keys := node.Key()
		var names []string
		offset := 0
		for keys.Next() {
			key := keys.Node()
			if len(names) == 0 {
				offset = int(key.Raw.Offset)
			}
			names = append(names, string(key.Data))
		}
		lineStart := bytes.LastIndexByte(original[:offset], '\n') + 1
		if !skip {
			out.Write(original[start:lineStart])
		}
		start = lineStart
		skip = len(names) >= 2 && names[0] == "mcp_servers" && names[1] == "pagehub"
		found = found || skip
	}
	if err := parser.Error(); err != nil {
		return nil, false, err
	}
	if !skip {
		out.Write(original[start:])
	}
	return out.Bytes(), found, nil
}

func decodeTOML(b []byte) (map[string]any, error) {
	m := map[string]any{}
	if err := toml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
func (c Client) codex(original []byte, connect bool) ([]byte, error) {
	before, err := decodeTOML(original)
	if err != nil {
		return nil, errors.New("invalid Codex TOML; left unchanged")
	}
	servers, err := object(before["mcp_servers"])
	if err != nil {
		return nil, err
	}
	entry := servers["pagehub"]
	if err = owned(entry, c.URL); err != nil {
		return nil, err
	}
	var out strings.Builder
	retained, found, err := withoutPagehubTables(original)
	if err != nil {
		return nil, err
	}
	out.Write(retained)
	if entry != nil && !found {
		return nil, errors.New("unsupported Codex pagehub table syntax; left unchanged")
	}
	if connect {
		if c.Token == "" {
			return nil, errors.New("management token is missing")
		}
		prefix := strings.TrimRight(out.String(), "\r\n")
		out.Reset()
		if prefix != "" {
			out.WriteString(prefix)
			out.WriteByte('\n')
		}
		server, e := object(entry)
		if e != nil {
			return nil, e
		}
		headers, e := object(server["http_headers"])
		if e != nil {
			return nil, e
		}
		server["url"] = c.URL
		headers["Authorization"] = "Bearer " + c.Token
		server["http_headers"] = headers
		block, e := toml.Marshal(server)
		if e != nil {
			return nil, errors.New("cannot encode Pagehub client settings")
		}
		out.WriteString("\n[mcp_servers.pagehub]\n")
		for _, line := range strings.SplitAfter(string(block), "\n") {
			if strings.HasPrefix(line, "[[") {
				line = "[[mcp_servers.pagehub." + line[2:]
			} else if strings.HasPrefix(line, "[") {
				line = "[mcp_servers.pagehub." + line[1:]
			}
			out.WriteString(line)
		}
	}
	after, err := decodeTOML([]byte(out.String()))
	if err != nil {
		return nil, err
	}
	afterServers, err := object(after["mcp_servers"])
	if err != nil {
		return nil, err
	}
	delete(servers, "pagehub")
	delete(afterServers, "pagehub")
	if len(servers) == 0 {
		delete(before, "mcp_servers")
	}
	if len(afterServers) == 0 {
		delete(after, "mcp_servers")
	}
	if !reflect.DeepEqual(before, after) {
		return nil, errors.New("unexpected unrelated Codex configuration change; left unchanged")
	}
	return []byte(out.String()), nil
}
func decodeJSON(b []byte) (map[string]any, error) {
	m := map[string]any{}
	if len(b) == 0 {
		return m, nil
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("configuration must be an object")
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("extra or invalid trailing JSON content")
	}
	return m, nil
}
func (c Client) claude(original []byte, connect bool) ([]byte, error) {
	m, err := decodeJSON(original)
	if err != nil {
		return nil, errors.New("invalid Claude JSON; left unchanged")
	}
	servers, err := object(m["mcpServers"])
	if err != nil {
		return nil, err
	}
	entry := servers["pagehub"]
	if err = owned(entry, c.URL); err != nil {
		return nil, err
	}
	if connect {
		if c.Token == "" {
			return nil, errors.New("management token is missing")
		}
		server, err := object(entry)
		if err != nil {
			return nil, err
		}
		headers, err := object(server["headers"])
		if err != nil {
			return nil, err
		}
		server["type"] = "http"
		server["url"] = c.URL
		headers["Authorization"] = "Bearer " + c.Token
		server["headers"] = headers
		servers["pagehub"] = server
		m["mcpServers"] = servers
	} else {
		if entry == nil {
			return original, nil
		}
		delete(servers, "pagehub")
		m["mcpServers"] = servers
	}
	b, err := json.MarshalIndent(m, "", "  ")
	return append(b, '\n'), err
}
func (c Client) Connected() (bool, error) {
	b, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var m map[string]any
	if c.Name == "codex" {
		m, err = decodeTOML(b)
	} else {
		m, err = decodeJSON(b)
	}
	if err != nil {
		return false, err
	}
	key := "mcp_servers"
	if c.Name == "claude" {
		key = "mcpServers"
	}
	servers, err := object(m[key])
	if err != nil {
		return false, err
	}
	entry := servers["pagehub"]
	if entry == nil {
		return false, nil
	}
	if err = owned(entry, c.URL); err != nil {
		return false, err
	}
	server, _ := object(entry)
	headers, err := object(server["http_headers"])
	if c.Name == "claude" {
		headers, err = object(server["headers"])
	}
	if err != nil {
		return false, err
	}
	return headers["Authorization"] == "Bearer "+c.Token, nil
}
