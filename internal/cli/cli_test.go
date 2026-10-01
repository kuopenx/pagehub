package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryCommandAndRepeatedExecution(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(home, "source")
	_ = os.WriteFile(binary, []byte("binary"), 0700)
	registered := false
	env := Environment{Home: home, Executable: binary, GOOS: "darwin", UID: 501, Runner: func(ctx context.Context, name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			if !registered {
				return "Could not find service", errors.New("missing")
			}
			return "state = running\n pid = 123", nil
		case "bootstrap":
			registered = true
		case "bootout":
			registered = false
		}
		return "", nil
	}, Ready: func(context.Context) error { return nil }, Open: func(context.Context, string) error { return nil }, Serve: func(context.Context, string, int) error { return nil }}
	commands := [][]string{{"--help"}, {"version", "--json"}, {"serve", "--port", "8766"}, {"setup", "--json"}, {"setup", "--json"}, {"service", "status", "--json"}, {"service", "stop"}, {"service", "stop"}, {"service", "start"}, {"service", "restart"}, {"connect", "codex", "--json"}, {"connect", "codex"}, {"connect", "claude"}, {"connect", "claude"}, {"open", "--json"}, {"disconnect", "codex"}, {"disconnect", "codex"}, {"disconnect", "claude"}, {"uninstall"}, {"uninstall"}}
	for _, args := range commands {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), args, &out, &stderr, env)
		if code != 0 {
			t.Fatalf("%v exit=%d: %s", args, code, stderr.String())
		}
		if strings.Contains(out.String(), "Bearer ") {
			t.Fatal("token leaked")
		}
		if strings.Contains(strings.Join(args, " "), "--json") {
			var value any
			if err := json.Unmarshal(out.Bytes(), &value); err != nil {
				t.Fatal("invalid JSON", err)
			}
		}
	}
	for _, args := range [][]string{{"unknown"}, {"serve", "--port", "-1"}, {"serve", "--port", "0"}, {"service", "wrong"}, {"connect", "wrong"}, {"version", "extra"}, {"serve", "--config-file", "x"}, {"serve", "--service-name", "../bad"}} {
		var out, stderr bytes.Buffer
		if Run(context.Background(), args, &out, &stderr, env) == 0 {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
	var out, stderr bytes.Buffer
	if Run(context.Background(), []string{"doctor", "--json"}, &out, &stderr, env) == 0 {
		t.Fatal("doctor accepted stopped service")
	}
	if !strings.Contains(out.String(), "checks") {
		t.Fatal("doctor output missing checks")
	}
}
func TestOpenHeadlessPrintsURLAndForeignRegistrationRejected(t *testing.T) {
	home := t.TempDir()
	env := Environment{Home: home, GOOS: "linux", Open: func(context.Context, string) error { return errors.New("no display") }}
	var out, stderr bytes.Buffer
	if Run(context.Background(), []string{"open", "--json"}, &out, &stderr, env) != 0 || !strings.Contains(out.String(), "http://127.0.0.1:8765/") {
		t.Fatal("headless fallback failed")
	}
}
