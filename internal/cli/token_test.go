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

type failedTokenOutput struct{}

func (failedTokenOutput) Write(p []byte) (int, error) {
	return 0, errors.New("rejected output: " + string(p))
}

func TestTokenCommandReportsOutputFailure(t *testing.T) {
	home := t.TempDir()
	env := Environment{Home: home, GOOS: "darwin"}
	for _, format := range []string{"text", "json"} {
		for _, action := range []string{"generate", "rotate", "show", "status", "list", "revoke"} {
			args := []string{"token", action}
			if format == "json" {
				args = append(args, "--json")
			}
			var stderr bytes.Buffer
			if code := Run(context.Background(), args, failedTokenOutput{}, &stderr, env); code != 1 {
				t.Fatalf("%s %s: output failure returned exit %d instead of 1", format, action, code)
			}
			if format == "json" {
				var value any
				if err := json.Unmarshal(stderr.Bytes(), &value); err != nil {
					t.Fatal("output failure did not report a JSON error")
				}
			} else if stderr.Len() == 0 {
				t.Fatal("output failure did not report an error")
			}
			if strings.Contains(stderr.String(), "rejected output:") {
				t.Fatal("output failure leaked writer error content")
			}
			if action == "generate" || action == "rotate" {
				var recovered, recoveryErr bytes.Buffer
				if code := Run(context.Background(), []string{"token", "show"}, &recovered, &recoveryErr, env); code != 0 || len(strings.TrimSpace(recovered.String())) != 64 {
					t.Fatal("could not recover committed credential after output failure")
				}
			}
		}
	}
}

func TestTokenCommands(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "private")
	env := Environment{Home: home, GOOS: "darwin"}
	run := func(action string, want int, format ...string) string {
		t.Helper()
		args := append([]string{"token", action, "--data-dir", dir}, format...)
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, &out, &stderr, env); code != want {
			t.Fatalf("token %s exit=%d want=%d", action, code, want)
		}
		if want != 0 && (out.Len() != 0 || stderr.Len() == 0) {
			t.Fatal("failed command must emit only an error on stderr")
		}
		return strings.TrimSpace(out.String())
	}
	if run("status", 0) != "missing" {
		t.Fatal("missing status")
	}
	run("show", 1)
	run("bad", 2)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read-only/invalid commands created credentials")
	}
	initial := run("generate", 0)
	if len(initial) != 64 || run("generate", 0) != initial || run("show", 0) != initial {
		t.Fatal("generate/show did not reuse the active credential")
	}
	status := run("status", 0, "--json")
	if strings.Contains(status, initial) || !strings.Contains(status, `"token_state":"active"`) {
		t.Fatal("status exposed a credential or omitted state")
	}
	// Revocation remains available even when service settings are damaged.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	var rotated result
	if err := json.Unmarshal([]byte(run("rotate", 0, "--json")), &rotated); err != nil || len(rotated.Token) != 64 || rotated.Token == initial || rotated.TokenState != "active" {
		t.Fatal("invalid rotate JSON", err)
	}
	for range 2 {
		if run("revoke", 0) != "revoked" || run("status", 0) != "revoked" {
			t.Fatal("repeated revoke failed")
		}
		run("show", 1)
	}
	regenerated := run("generate", 0)
	if len(regenerated) != 64 || regenerated == rotated.Token || regenerated == initial {
		t.Fatal("generation reused a revoked credential")
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"show", "status", "generate"} {
		run(action, 1, "--json")
	}
	if len(run("rotate", 0)) != 64 {
		t.Fatal("rotation did not repair invalid credential")
	}
	info, err := os.Stat(filepath.Join(dir, "token"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions", err)
	}
}

func TestRevokedTokenCannotBeRegistered(t *testing.T) {
	home := t.TempDir()
	env := Environment{Home: home, GOOS: "darwin"}
	var out, stderr bytes.Buffer
	if Run(context.Background(), []string{"token", "revoke"}, &out, &stderr, env) != 0 {
		t.Fatal("revoke failed")
	}
	out.Reset()
	stderr.Reset()
	if Run(context.Background(), []string{"connect", "codex"}, &out, &stderr, env) != 1 || !strings.Contains(stderr.String(), "revoked") {
		t.Fatal("revoked token was registered")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("revoked credentials changed client configuration")
	}
}

func TestNamedTokenCommandsAndSafeListing(t *testing.T) {
	home := t.TempDir()
	env := Environment{Home: home, GOOS: "darwin"}
	run := func(action, name string, want int, flags ...string) string {
		t.Helper()
		args := []string{"token", action}
		if name != "" {
			args = append(args, "--name", name)
		}
		args = append(args, flags...)
		var out, stderr bytes.Buffer
		if Run(context.Background(), args, &out, &stderr, env) != want {
			t.Fatalf("token %s for %s returned unexpected exit code", action, name)
		}
		if want != 0 && out.Len() != 0 {
			t.Fatal("failed command exposed output")
		}
		return strings.TrimSpace(out.String())
	}
	legacy := run("generate", "", 0)
	phone := run("generate", "phone", 0)
	laptop := run("generate", "laptop", 0)
	if len(phone) != 64 || len(laptop) != 64 || phone == laptop || legacy == phone || legacy == laptop {
		t.Fatal("device tokens not unique")
	}
	if run("show", "phone", 0) != phone || run("generate", "phone", 0) != phone {
		t.Fatal("named token not preserved")
	}
	for _, flags := range [][]string{nil, {"--json"}} {
		listed := run("list", "", 0, flags...)
		for _, token := range []string{legacy, phone, laptop} {
			if strings.Contains(listed, token) {
				t.Fatal("list exposed credentials")
			}
		}
		if !strings.Contains(listed, "phone") || !strings.Contains(listed, "laptop") || !strings.Contains(listed, "default") {
			t.Fatal("token list omitted a device")
		}
	}
	for range 2 {
		if run("revoke", "phone", 0) != "revoked" || run("status", "phone", 0) != "revoked" {
			t.Fatal("device revocation failed")
		}
	}
	run("show", "phone", 1)
	if run("show", "laptop", 0) != laptop || run("show", "", 0) != legacy {
		t.Fatal("revoke changed other devices")
	}
	rotated := run("rotate", "laptop", 0)
	if rotated == laptop || len(rotated) != 64 || run("show", "", 0) != legacy {
		t.Fatal("rotation changed unrelated credentials")
	}
	if run("status", "missing", 0) != "missing" {
		t.Fatal("unknown device status")
	}
	for _, action := range []string{"show", "rotate", "revoke"} {
		run(action, "missing", 1)
	}
	for _, name := range []string{"../phone", "phone\n", "phone name"} {
		run("generate", name, 2)
	}
	run("list", "phone", 2)
	var out, stderr bytes.Buffer
	if Run(context.Background(), []string{"open", "--name", "phone"}, &out, &stderr, env) != 2 {
		t.Fatal("--name accepted outside token commands")
	}
}
