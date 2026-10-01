package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kuopenx/pagehub/internal/config"
)

func TestWaitRequiresCurrentPagehubProcess(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		valid, redirect bool
	}{
		{name: "plain HTTP 200", body: "not Pagehub"},
		{name: "other Pagehub PID", body: `{"service":"pagehub","status":"ok","version":"test","pid":43}`},
		{name: "missing PID", body: `{"service":"pagehub","status":"ok","version":"test"}`},
		{name: "missing identity", body: `{"status":"ok","version":"test","pid":42}`},
		{name: "extra JSON", body: `{"service":"pagehub","status":"ok","version":"test","pid":42} {}`},
		{name: "redirect", redirect: true},
		{name: "current process", valid: true, body: `{"service":"pagehub","status":"ok","version":"test","pid":42}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var destinationCalls atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destinationCalls.Add(1)
				fmt.Fprint(w, `{"service":"pagehub","status":"ok","version":"test","pid":42}`)
			}))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.redirect {
					http.Redirect(w, r, destination.URL, 302)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			settings := config.Default()
			settings.Port = server.Listener.Addr().(*net.TCPAddr).Port
			m := Manager{GOOS: "darwin", Settings: settings, Run: func(context.Context, string, ...string) (string, error) { return "state = running\n pid = 42", nil }}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			err := m.Wait(ctx)
			if tc.valid && err != nil {
				t.Fatal("current process rejected", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("unrelated or invalid health accepted")
			}
			if destinationCalls.Load() != 0 {
				t.Fatal("health redirect followed")
			}
		})
	}
}

func TestServiceRenameMigratesAndRollsBack(t *testing.T) {
	for _, failure := range []string{"none", "bootstrap", "health"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			states := map[string]bool{}
			failNextBootstrap := false
			runner := func(ctx context.Context, name string, args ...string) (string, error) {
				label := filepath.Base(args[len(args)-1])
				if args[0] == "bootstrap" {
					label = strings.TrimSuffix(label, ".plist")
				}
				switch args[0] {
				case "print":
					if !states[label] {
						return "Could not find service", errors.New("missing")
					}
					return "state = running\n pid = 42", nil
				case "bootstrap":
					if failNextBootstrap {
						failNextBootstrap = false
						return "", errors.New("bootstrap failed")
					}
					for other, running := range states {
						if running && other != label {
							t.Fatal("started before old service stopped")
						}
					}
					states[label] = true
				case "bootout":
					states[label] = false
				}
				return "", nil
			}
			old := Manager{Home: dir, DataDir: filepath.Join(dir, "data"), GOOS: "darwin", UID: 501, Settings: config.Default(), Run: runner, Ready: func(context.Context) error { return nil }}
			source := filepath.Join(dir, "source")
			if err := os.WriteFile(source, []byte("old binary"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := old.Install(context.Background(), source); err != nil {
				t.Fatal(err)
			}
			previousPlist, _ := os.ReadFile(old.Plist())
			renamed := old
			renamed.Settings.ServiceName = "io.pagehub.renamed"
			renamed.Settings.Port = 9876
			if err := os.WriteFile(source, []byte("new binary"), 0700); err != nil {
				t.Fatal(err)
			}
			failNextBootstrap = failure == "bootstrap"
			if failure == "health" {
				renamed.Ready = func(context.Context) error { return errors.New("health failed") }
			}
			err := renamed.Install(context.Background(), source)
			saved, loadErr := config.Load(old.DataDir)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			binary, _ := os.ReadFile(old.Binary())
			if failure != "none" {
				if err == nil {
					t.Fatal("failed migration succeeded")
				}
				if !states[old.Settings.ServiceName] || states[renamed.Settings.ServiceName] {
					t.Fatal("old registration not restored", states)
				}
				restoredPlist, _ := os.ReadFile(old.Plist())
				if saved != old.Settings || string(binary) != "old binary" || string(restoredPlist) != string(previousPlist) {
					t.Fatal("old installation not restored")
				}
				if _, err = os.Stat(renamed.Plist()); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("new plist remains")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if states[old.Settings.ServiceName] || !states[renamed.Settings.ServiceName] {
					t.Fatal("two service registrations", states)
				}
				if saved != renamed.Settings || string(binary) != "new binary" {
					t.Fatal("migration not saved")
				}
				if _, err = os.Stat(old.Plist()); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("old plist remains")
				}
				if err = renamed.Install(context.Background(), source); err != nil {
					t.Fatal("repeat setup failed", err)
				}
				if err = renamed.Uninstall(context.Background()); err != nil {
					t.Fatal(err)
				}
				for _, running := range states {
					if running {
						t.Fatal("uninstall left registration")
					}
				}
			}
		})
	}
}

func TestRenameRejectsForeignPreviousPlistBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	original := config.Default()
	if err := config.Save(dir, original); err != nil {
		t.Fatal(err)
	}
	m := Manager{Home: t.TempDir(), DataDir: dir, GOOS: "darwin", Settings: original, Run: func(context.Context, string, ...string) (string, error) {
		return "Could not find service", errors.New("missing")
	}}
	if err := config.AtomicWrite(m.Plist(), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.AtomicWrite(m.Binary(), []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	m.Settings.ServiceName = "io.pagehub.renamed"
	if err := m.Install(context.Background(), source); err == nil {
		t.Fatal("foreign previous plist accepted")
	}
	saved, err := config.Load(dir)
	if err != nil || saved != original {
		t.Fatal("settings changed on failure", err)
	}
	binary, _ := os.ReadFile(m.Binary())
	if string(binary) != "old" {
		t.Fatal("binary changed before ownership validation")
	}
}

func TestFreshCustomInstanceDoesNotMigrateUnrelatedDefaultService(t *testing.T) {
	home := t.TempDir()
	defaultService := Manager{Home: home, DataDir: t.TempDir(), GOOS: "darwin", Settings: config.Default()}
	if err := config.AtomicWrite(defaultService.Plist(), defaultService.plist(), 0600); err != nil {
		t.Fatal(err)
	}
	previousPlist, _ := os.ReadFile(defaultService.Plist())
	registered := false
	custom := Manager{Home: home, DataDir: t.TempDir(), GOOS: "darwin", Settings: config.Default(), Ready: func(context.Context) error { return nil }}
	custom.Settings.ServiceName = "io.pagehub.isolated"
	custom.Run = func(ctx context.Context, name string, args ...string) (string, error) {
		if strings.Contains(args[len(args)-1], config.DefaultLabel) {
			t.Fatal("queried unrelated default service")
		}
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
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := custom.Install(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	currentPlist, _ := os.ReadFile(defaultService.Plist())
	if string(currentPlist) != string(previousPlist) {
		t.Fatal("unrelated default service modified")
	}
}

func TestFailedRollbackStopCannotStartSecondService(t *testing.T) {
	dir := t.TempDir()
	states := map[string]bool{}
	rejectStop := false
	m := Manager{Home: dir, DataDir: filepath.Join(dir, "data"), GOOS: "darwin", Settings: config.Default(), Ready: func(context.Context) error { return nil }}
	m.Run = func(ctx context.Context, name string, args ...string) (string, error) {
		label := filepath.Base(args[len(args)-1])
		if args[0] == "bootstrap" {
			label = strings.TrimSuffix(label, ".plist")
		}
		switch args[0] {
		case "print":
			if !states[label] {
				return "Could not find service", errors.New("missing")
			}
			return "state = running", nil
		case "bootstrap":
			for other, running := range states {
				if running && other != label {
					t.Fatal("rollback started second service")
				}
			}
			states[label] = true
		case "bootout":
			if rejectStop && label == "io.pagehub.renamed" {
				return "", errors.New("cannot stop replacement")
			}
			states[label] = false
		}
		return "", nil
	}
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("old binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	renamed := m
	renamed.Settings.ServiceName = "io.pagehub.renamed"
	renamed.Ready = func(context.Context) error { rejectStop = true; return errors.New("health failed") }
	if err := os.WriteFile(source, []byte("new binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := renamed.Install(context.Background(), source); err == nil || !strings.Contains(err.Error(), "rollback could not stop") {
		t.Fatal("rollback stop failure not reported", err)
	}
	if states[m.Settings.ServiceName] || !states[renamed.Settings.ServiceName] {
		t.Fatal("unexpected active registrations", states)
	}
	saved, err := config.Load(m.DataDir)
	binary, _ := os.ReadFile(m.Binary())
	if err != nil || saved != renamed.Settings || string(binary) != "new binary" {
		t.Fatal("files changed while replacement remained running", err)
	}
}
