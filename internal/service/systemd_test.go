package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuopenx/pagehub/internal/config"
)

type fakeSystemd struct {
	t                       *testing.T
	home                    string
	running, enabled        map[string]bool
	fail, fragment, dropins string
	loadState               string
	failed, rateLimit       bool
	stops                   int
}

func (f *fakeSystemd) run(_ context.Context, name string, args ...string) (string, error) {
	f.t.Helper()
	if name != "systemctl" || args[0] != "--user" {
		f.t.Fatalf("unexpected command: %s %v", name, args)
	}
	verb := args[1]
	if f.fail == verb {
		f.fail = ""
		return "", errors.New("injected " + verb)
	}
	if verb == "daemon-reload" {
		return "", nil
	}
	unit := args[2]
	path := filepath.Join(f.home, ".config", "systemd", "user", unit)
	switch verb {
	case "show":
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return "LoadState=not-found\n", nil
		}
		state, pid := "inactive", 0
		if f.failed {
			state = "failed"
		}
		if f.running[unit] {
			state, pid = "active", 42
		}
		enabled := "disabled"
		if f.enabled[unit] {
			enabled = "enabled"
		}
		if f.fragment != "" {
			path = f.fragment
		}
		loadState := f.loadState
		if loadState == "" {
			loadState = "loaded"
		}
		result := "success"
		if f.rateLimit {
			result = "start-limit-hit"
		}
		return "Result=" + result + "\n" + fmt.Sprintf("LoadState=%s\nActiveState=%s\nMainPID=%d\nUnitFileState=%s\nFragmentPath=%s\nDropInPaths=%s\n", loadState, state, pid, enabled, path, f.dropins), nil
	case "reset-failed":
		f.failed = false
		f.rateLimit = false
	case "start":
		if f.rateLimit {
			f.failed = true
			return "", errors.New("start limit reached")
		}
		for other, running := range f.running {
			if other != unit && running {
				f.t.Fatal("started replacement before stopping previous service")
			}
		}
		f.running[unit] = true
	case "stop":
		f.running[unit] = false
		f.stops++
	case "enable":
		f.enabled[unit] = true
	case "disable":
		f.enabled[unit] = false
	default:
		f.t.Fatalf("unexpected verb: %s", verb)
	}
	return "", nil
}
func linuxManager(t *testing.T) (Manager, *fakeSystemd, string) {
	t.Helper()
	home := t.TempDir()
	f := &fakeSystemd{t: t, home: home, running: map[string]bool{}, enabled: map[string]bool{}}
	m := Manager{Home: home, DataDir: filepath.Join(home, "data"), GOOS: "linux", Settings: config.Default(), Run: f.run, Ready: func(context.Context) error { return nil }}
	source := filepath.Join(home, "source")
	if err := os.WriteFile(source, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	return m, f, source
}
func TestSystemdLifecycle(t *testing.T) {
	ctx := context.Background()
	m, f, source := linuxManager(t)
	for range 2 {
		if err := m.Install(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	s, err := m.Status(ctx)
	if err != nil || !s.Running || !s.Enabled || s.PID != 42 {
		t.Fatal(s, err)
	}
	for range 2 {
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := m.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !f.enabled[m.unitName()] {
		t.Fatal("stop disabled login startup")
	}
	if err := m.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(m.DataDir, "token")
	_ = os.WriteFile(token, []byte("test token"), 0600)
	for range 2 {
		if err := m.Uninstall(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if f.running[m.unitName()] || f.enabled[m.unitName()] {
		t.Fatal("service remains")
	}
	for _, path := range []string{m.Binary(), m.serviceFile()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("file remains", path)
		}
	}
	if _, err := os.Stat(token); err != nil {
		t.Fatal("token removed")
	}
	if err := m.Start(ctx); err == nil {
		t.Fatal("start without installation accepted")
	}
}
func TestSystemdUpgradeRollback(t *testing.T) {
	for _, failure := range []string{"start", "enable", "health"} {
		for _, running := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/running=%t", failure, running), func(t *testing.T) {
				ctx := context.Background()
				m, f, source := linuxManager(t)
				if err := m.Install(ctx, source); err != nil {
					t.Fatal(err)
				}
				f.running[m.unitName()] = running
				f.enabled[m.unitName()] = running
				oldUnit, _ := os.ReadFile(m.serviceFile())
				oldSettings, _ := os.ReadFile(filepath.Join(m.DataDir, "settings.json"))
				_ = os.WriteFile(source, []byte("new"), 0700)
				m.Settings.Port++
				if failure == "health" {
					m.Ready = func(context.Context) error { return errors.New("unhealthy") }
				} else {
					f.fail = failure
				}
				if err := m.Install(ctx, source); err == nil {
					t.Fatal("bad upgrade accepted")
				}
				binary, _ := os.ReadFile(m.Binary())
				unit, _ := os.ReadFile(m.serviceFile())
				settings, _ := os.ReadFile(filepath.Join(m.DataDir, "settings.json"))
				if string(binary) != "old" || string(unit) != string(oldUnit) || string(settings) != string(oldSettings) || f.running[m.unitName()] != running || f.enabled[m.unitName()] != running {
					t.Fatal("previous installation/state not restored")
				}
			})
		}
	}
}
func TestSystemdRenameAndRollback(t *testing.T) {
	ctx := context.Background()
	m, f, source := linuxManager(t)
	if err := m.Install(ctx, source); err != nil {
		t.Fatal(err)
	}
	prior := m
	m.Settings.ServiceName = "io.pagehub.renamed"
	m.Settings.Port++
	m.Ready = func(context.Context) error { return errors.New("unhealthy") }
	if err := m.Install(ctx, source); err == nil {
		t.Fatal("bad migration accepted")
	}
	if !f.running[prior.unitName()] || !f.enabled[prior.unitName()] || f.running[m.unitName()] || f.enabled[m.unitName()] {
		t.Fatal("migration rollback failed")
	}
	m.Ready = func(context.Context) error { return nil }
	if err := m.Install(ctx, source); err != nil {
		t.Fatal(err)
	}
	if f.running[prior.unitName()] || f.enabled[prior.unitName()] || !f.running[m.unitName()] || !f.enabled[m.unitName()] {
		t.Fatal("migration failed")
	}
	if _, err := os.Stat(prior.serviceFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prior unit remains")
	}
}
func TestSystemdForeignUnitAndUnavailableManager(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"file", "fragment", "dropin", "bus"} {
		t.Run(kind, func(t *testing.T) {
			m, f, source := linuxManager(t)
			if err := m.Install(ctx, source); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				_ = os.WriteFile(m.serviceFile(), append(m.serviceContent(), []byte("\nExecStartPost=/bin/false\n")...), 0600)
			case "fragment":
				f.fragment = "/usr/lib/systemd/user/foreign.service"
			case "dropin":
				f.dropins = "/tmp/foreign.conf"
			case "bus":
				m.Run = func(context.Context, string, ...string) (string, error) { return "", errors.New("no bus") }
			}
			stops := f.stops
			if err := m.Install(ctx, source); err == nil {
				t.Fatal("unsafe installation accepted")
			}
			if err := m.Stop(ctx); err == nil {
				t.Fatal("unsafe stop accepted")
			}
			if f.stops != stops {
				t.Fatal("foreign unit stopped")
			}
		})
	}
}
func TestSystemdFirstInstallRollback(t *testing.T) {
	m, f, source := linuxManager(t)
	m.Ready = func(context.Context) error { return errors.New("bad health") }
	if err := m.Install(context.Background(), source); err == nil {
		t.Fatal("unhealthy install accepted")
	}
	for _, path := range []string{m.Binary(), m.serviceFile(), filepath.Join(m.DataDir, "settings.json")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("new file remains", path)
		}
	}
	if f.running[m.unitName()] || f.enabled[m.unitName()] {
		t.Fatal("new service remains")
	}
}
func TestSystemdEscapesPaths(t *testing.T) {
	m, _, _ := linuxManager(t)
	m.DataDir = filepath.Join(m.Home, "space % $HOME \"quote\" \\ slash\nnewline")
	content := string(m.serviceContent())
	if !strings.Contains(content, `ExecStart=:/usr/bin/env -- "`) || !strings.Contains(content, `%%`) || !strings.Contains(content, `$HOME`) || !strings.Contains(content, `\"quote\"`) || !strings.Contains(content, `\\ slash\nnewline`) {
		t.Fatal("paths not escaped", content)
	}
	if err := config.AtomicWrite(m.serviceFile(), m.serviceContent(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.owned(); err != nil {
		t.Fatal(err)
	}
}

func TestSystemdUnitPathUsesAbsoluteXDGConfigHome(t *testing.T) {
	m, _, _ := linuxManager(t)
	t.Setenv("HOME", m.Home)
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	if got, want := m.serviceFile(), filepath.Join(root, "systemd", "user", m.unitName()); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if got, want := m.serviceFile(), filepath.Join(m.Home, ".config", "systemd", "user", m.unitName()); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestSystemdBadSettingRollsBackWithoutStoppingLiveProcess(t *testing.T) {
	ctx := context.Background()
	m, f, source := linuxManager(t)
	f.loadState = "bad-setting"
	if err := m.Install(ctx, source); err == nil {
		t.Fatal("invalid unit installed")
	}
	for _, path := range []string{m.Binary(), m.serviceFile(), filepath.Join(m.DataDir, "settings.json")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed installation remains", path)
		}
	}
	if err := config.AtomicWrite(m.serviceFile(), m.serviceContent(), 0600); err != nil {
		t.Fatal(err)
	}
	f.running[m.unitName()] = true
	if err := m.Stop(ctx); err == nil {
		t.Fatal("ignored running process with invalid unit")
	}
	if f.stops != 0 {
		t.Fatal("unexpected stop on invalid live unit")
	}
}

func TestSystemdExplicitStartRecoversFailedStateAndRateLimit(t *testing.T) {
	ctx := context.Background()
	m, f, source := linuxManager(t)
	if err := m.Install(ctx, source); err != nil {
		t.Fatal(err)
	}
	f.running[m.unitName()] = false
	f.failed = true
	f.fail = "reset-failed"
	if err := m.Start(ctx); err == nil {
		t.Fatal("failed reset accepted")
	}
	if f.running[m.unitName()] {
		t.Fatal("started without clearing failed state")
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.running[m.unitName()] || f.failed {
		t.Fatal("failed unit not recovered")
	}
	f.running[m.unitName()] = false
	f.rateLimit = true
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !f.running[m.unitName()] || f.rateLimit {
		t.Fatal("start-limit-hit not recovered")
	}
}
