package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (m Manager) unitName() string { return m.Settings.ServiceName + ".service" }
func (m Manager) serviceFile() string {
	if m.platform() == "linux" {
		// systemd's user search path honors XDG_CONFIG_HOME. Tests with an explicit
		// home remain isolated from the invoking user's environment.
		root := filepath.Join(m.Home, ".config")
		if home, err := os.UserHomeDir(); err == nil && home == m.Home {
			if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
				root = xdg
			}
		}
		return filepath.Join(root, "systemd", "user", m.unitName())
	}
	return m.Plist()
}

// systemd command lines have their own quoting and expansion rules; they are
// never interpreted by a shell. Escape specifiers; the : command prefix
// disables environment expansion.
func systemdArg(s string) string {
	s = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(s)
	return "\"" + s + "\""
}
func (m Manager) serviceContent() []byte {
	if m.platform() != "linux" {
		return m.plist()
	}
	// systemd restricts characters in the executable path itself. env accepts
	// the managed path as an argument and execs it without a shell or a new PID.
	return []byte(fmt.Sprintf(`[Unit]
Description=Pagehub single-file HTML service

[Service]
Type=exec
ExecStart=:/usr/bin/env -- %s serve --data-dir %s --port %d
Restart=always
RestartSec=2
UMask=0077
StandardOutput=null
StandardError=null

[Install]
WantedBy=default.target
`, systemdArg(m.Binary()), systemdArg(m.DataDir), m.Settings.Port))
}
func (m Manager) systemctl(ctx context.Context, args ...string) (string, error) {
	out, err := m.runner()(ctx, "systemctl", append([]string{"--user"}, args...)...)
	if err != nil {
		return out, fmt.Errorf("user systemd unavailable or operation failed (requires systemd and an active user session; use pagehub serve for foreground operation): %w", err)
	}
	return out, nil
}
func (m Manager) systemdReload(ctx context.Context) error {
	_, err := m.systemctl(ctx, "daemon-reload")
	return err
}
func (m Manager) systemdOwned() error {
	b, err := os.ReadFile(m.serviceFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// Require the owned executable and data directory in the command.
	// Port can legitimately change during setup; only the owned executable and
	// data directory determine installation ownership.
	prefix := "ExecStart=:/usr/bin/env -- " + systemdArg(m.Binary()) + " serve --data-dir " + systemdArg(m.DataDir) + " --port "
	commands := 0
	owned := m
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			commands++
			if !strings.HasPrefix(line, prefix) {
				return errors.New("existing systemd unit belongs to another installation")
			}
			port, e := strconv.Atoi(strings.TrimPrefix(line, prefix))
			if e != nil || port < 1 || port > 65535 {
				return errors.New("existing systemd unit has an unexpected command")
			}
			owned.Settings.Port = port
		}
	}
	if commands != 1 {
		return errors.New("existing systemd unit belongs to another installation")
	}
	if string(b) != string(owned.serviceContent()) {
		return errors.New("existing systemd unit was modified; left unchanged")
	}
	return nil
}

// Distribution-wide defaults do not change installation ownership. Accept only
// vendor user-service defaults, never unit-specific, administrator, or user
// overrides. Pagehub neither writes nor removes these files.
func systemdVendorDropIns(paths string) bool {
	for _, path := range strings.Fields(paths) {
		if filepath.Clean(path) != path || !strings.HasSuffix(path, ".conf") {
			return false
		}
		switch filepath.Dir(path) {
		case "/usr/lib/systemd/user/service.d", "/lib/systemd/user/service.d":
		default:
			return false
		}
	}
	return true
}

func (m Manager) systemdStatus(ctx context.Context) (Status, error) {
	s := Status{Label: m.Settings.ServiceName, Binary: m.Binary()}
	out, err := m.systemctl(ctx, "show", m.unitName(), "--property=LoadState,ActiveState,MainPID,UnitFileState,FragmentPath,DropInPaths,Result", "--no-pager", "--all")
	if err != nil {
		return s, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			props[k] = v
		}
	}
	if props["LoadState"] == "not-found" {
		return s, nil
	}
	if props["FragmentPath"] != m.serviceFile() || !systemdVendorDropIns(props["DropInPaths"]) {
		return s, errors.New("systemd unit or drop-in belongs to another installation; left unchanged")
	}
	s.loadState = props["LoadState"]
	s.systemdResult = props["Result"]
	s.Registered = true
	s.activeState = props["ActiveState"]
	s.Running = props["ActiveState"] == "active"
	s.PID, err = strconv.Atoi(props["MainPID"])
	if err != nil || s.PID < 0 {
		return s, errors.New("invalid systemd MainPID")
	}
	s.Enabled = props["UnitFileState"] == "enabled"
	if s.loadState != "loaded" {
		return s, fmt.Errorf("systemd unit is not loaded: %s", s.loadState)
	}
	return s, nil
}
func (m Manager) systemdStop(ctx context.Context) error {
	if err := m.systemdOwned(); err != nil {
		return err
	}
	s, err := m.Status(ctx)
	if err != nil {
		// An owned replacement that failed to parse cannot have started. Permit
		// rollback to restore the old files, but never ignore a live process.
		if s.loadState == "bad-setting" && s.PID == 0 && (s.activeState == "inactive" || s.activeState == "failed") {
			return nil
		}
		return err
	}
	if !s.Registered {
		return nil
	}
	if _, err = os.Stat(m.serviceFile()); err != nil {
		return errors.New("registered service has no owned systemd unit file; left unchanged")
	}
	// systemctl waits for the stop job; KillMode=control-group is the default.
	if _, err = m.systemctl(ctx, "stop", m.unitName()); err != nil {
		return err
	}
	s, err = m.Status(ctx)
	if err != nil {
		return err
	}
	if s.Registered && s.activeState != "inactive" && s.activeState != "failed" || s.PID != 0 {
		return errors.New("service did not fully stop; not starting another process")
	}
	return nil
}
func (m Manager) systemdStart(ctx context.Context) error {
	if err := m.systemdOwned(); err != nil {
		return err
	}
	if _, err := os.Stat(m.serviceFile()); err != nil {
		return errors.New("service is not installed; run pagehub setup")
	}
	if err := m.systemdReload(ctx); err != nil {
		return err
	}
	s, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if s.Running {
		return nil
	}
	// Failed units retain systemd's restart counters. Fresh inactive units can
	// be garbage-collected immediately, so reset-failed applies only to failures.
	reset := func() error { _, e := m.systemctl(ctx, "reset-failed", m.unitName()); return e }
	if s.activeState == "failed" {
		if err = reset(); err != nil {
			return err
		}
	}
	_, err = m.systemctl(ctx, "start", m.unitName())
	if err != nil {
		failed, statusErr := m.Status(ctx)
		if statusErr == nil && failed.systemdResult == "start-limit-hit" {
			if resetErr := reset(); resetErr != nil {
				return errors.Join(err, resetErr)
			}
			_, err = m.systemctl(ctx, "start", m.unitName())
		}
	}
	return err
}
func (m Manager) systemdEnable(ctx context.Context) error {
	_, err := m.systemctl(ctx, "enable", m.unitName())
	return err
}
func (m Manager) systemdDisable(ctx context.Context) error {
	if _, err := os.Stat(m.serviceFile()); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, err := m.systemctl(ctx, "disable", m.unitName())
	return err
}
func (m Manager) restoreService(ctx context.Context, s Status) error {
	if m.platform() != "linux" {
		if s.Registered {
			return m.Start(ctx)
		}
		return nil
	}
	if err := m.systemdReload(ctx); err != nil {
		return err
	}
	if s.Enabled {
		if err := m.systemdEnable(ctx); err != nil {
			return err
		}
	}
	if s.Running {
		return m.Start(ctx)
	}
	return nil
}
