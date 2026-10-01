package service

import (
	"context"
	"errors"
	"github.com/kuopenx/pagehub/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceInstallLifecycleAndRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	registered := false
	failBootstrap := false
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			if !registered {
				return "Could not find service", errors.New("not found")
			}
			return "state = running\n pid = 42", nil
		case "bootstrap":
			if failBootstrap {
				failBootstrap = false
				return "failed", errors.New("bootstrap failed")
			}
			registered = true
		case "bootout":
			registered = false
		}
		return "", nil
	}
	m := Manager{Home: dir, DataDir: filepath.Join(dir, "data"), GOOS: "darwin", UID: 501, Settings: config.Default(), Run: runner, Ready: func(context.Context) error { return nil }}
	source := filepath.Join(dir, "source")
	_ = os.WriteFile(source, []byte("old binary"), 0700)
	if err := m.Install(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := m.Status(ctx)
	if err != nil || !s.Running || s.PID != 42 {
		t.Fatal("status", err)
	}
	b, _ := os.ReadFile(m.Plist())
	if !strings.Contains(string(b), "<string>serve</string>") {
		t.Fatal("wrong service args")
	}
	for range 2 {
		if err = m.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(source, []byte("new binary"), 0700)
	failBootstrap = true
	if err = m.Install(ctx, source); err == nil {
		t.Fatal("failed installation accepted")
	}
	b, _ = os.ReadFile(m.Binary())
	if string(b) != "old binary" || !registered {
		t.Fatal("rollback failed")
	}
	for range 2 {
		if err = m.Uninstall(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(m.Binary()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("binary remains")
	}
	if err = m.Start(ctx); err == nil {
		t.Fatal("start without installation accepted")
	}
}
func TestForeignServiceAndUnsupportedPlatform(t *testing.T) {
	m := Manager{Home: t.TempDir(), DataDir: t.TempDir(), GOOS: "linux", Settings: config.Default()}
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("unsupported platform accepted")
	}
	m.GOOS = "darwin"
	_ = config.AtomicWrite(m.Plist(), []byte("foreign"), 0600)
	if err := m.Stop(context.Background()); err == nil {
		t.Fatal("foreign service stopped")
	}
}

func TestHealthFailureRestoresPreviousInstallation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	registered := false
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			if !registered {
				return "Could not find service", errors.New("missing")
			}
			return "state = running", nil
		case "bootstrap":
			registered = true
		case "bootout":
			registered = false
		}
		return "", nil
	}
	m := Manager{Home: dir, DataDir: filepath.Join(dir, "data"), GOOS: "darwin", UID: 501, Settings: config.Default(), Run: runner, Ready: func(context.Context) error { return nil }}
	source := filepath.Join(dir, "source")
	_ = os.WriteFile(source, []byte("old"), 0700)
	if err := m.Install(ctx, source); err != nil {
		t.Fatal(err)
	}
	oldPlist, _ := os.ReadFile(m.Plist())
	_ = os.WriteFile(source, []byte("bad new"), 0700)
	m.Ready = func(context.Context) error { return errors.New("health failed") }
	if err := m.Install(ctx, source); err == nil {
		t.Fatal("unhealthy setup accepted")
	}
	b, _ := os.ReadFile(m.Binary())
	plist, _ := os.ReadFile(m.Plist())
	if string(b) != "old" || string(plist) != string(oldPlist) || !registered {
		t.Fatal("health rollback failed")
	}
}

func TestLegacyMigrationAndRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	states := map[string]bool{}
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		if args[0] == "bootstrap" {
			b, _ := os.ReadFile(args[2])
			label := config.DefaultLabel
			if strings.Contains(string(b), config.LegacyLabel) {
				label = config.LegacyLabel
			}
			states[label] = true
			return "", nil
		}
		label := config.DefaultLabel
		if strings.Contains(args[len(args)-1], config.LegacyLabel) {
			label = config.LegacyLabel
		}
		switch args[0] {
		case "print":
			if !states[label] {
				return "Could not find service", errors.New("missing")
			}
			return "state = running", nil
		case "bootout":
			states[label] = false
		}
		return "", nil
	}
	m := Manager{Home: dir, DataDir: filepath.Join(dir, "data"), GOOS: "darwin", UID: 501, Settings: config.Default(), Run: runner, Ready: func(context.Context) error { return errors.New("bad upgrade") }}
	legacy := m
	legacy.Settings.ServiceName = config.LegacyLabel
	_ = config.AtomicWrite(m.Binary(), []byte("legacy binary"), 0700)
	_ = config.AtomicWrite(legacy.Plist(), legacy.plist(), 0600)
	states[config.LegacyLabel] = true
	source := filepath.Join(dir, "new")
	_ = os.WriteFile(source, []byte("new binary"), 0700)
	if err := m.Install(ctx, source); err == nil {
		t.Fatal("unhealthy migration accepted")
	}
	if !states[config.LegacyLabel] || states[config.DefaultLabel] {
		t.Fatal("legacy service was not restored")
	}
	b, _ := os.ReadFile(m.Binary())
	if string(b) != "legacy binary" {
		t.Fatal("legacy binary was not restored")
	}
	m.Ready = func(context.Context) error { return nil }
	if err := m.Install(ctx, source); err != nil {
		t.Fatal(err)
	}
	if states[config.LegacyLabel] || !states[config.DefaultLabel] {
		t.Fatal("two service processes remain")
	}
	if _, err := os.Stat(legacy.Plist()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy registration remains")
	}
}

func TestRestartWaitsForRegistrationAndOldProcessExit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	registered := true
	unloading := false
	polls := 0
	processPolls := 0
	bootstrap := false
	m := Manager{Home: dir, DataDir: filepath.Join(dir, "data"), GOOS: "darwin", UID: 501, Settings: config.Default()}
	m.PIDAlive = func(int) bool { processPolls++; return processPolls < 3 }
	m.Run = func(ctx context.Context, name string, args ...string) (string, error) {
		switch args[0] {
		case "print":
			if unloading {
				polls++
				if polls >= 3 {
					registered = false
				}
			}
			if !registered {
				return "Could not find service", errors.New("missing")
			}
			return "state = running\n pid = 99999", nil
		case "bootout":
			unloading = true
		case "bootstrap":
			if registered || processPolls < 3 {
				t.Fatal("bootstrap before old registration/process exited")
			}
			bootstrap = true
			registered = true
			unloading = false
		}
		return "", nil
	}
	_ = config.AtomicWrite(m.Plist(), m.plist(), 0600)
	if err := m.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if !bootstrap || polls < 3 || processPolls < 3 {
		t.Fatal("restart skipped teardown wait")
	}
}
