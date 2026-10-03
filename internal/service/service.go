package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/kuopenx/pagehub/internal/config"
	"github.com/kuopenx/pagehub/internal/localhttp"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Runner func(context.Context, string, ...string) (string, error)

func Exec(ctx context.Context, name string, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(b), fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

type Manager struct {
	Home, DataDir, GOOS string
	UID                 int
	Settings            config.Settings
	Run                 Runner
	Ready               func(context.Context) error
	PIDAlive            func(int) bool
}
type Status struct {
	activeState   string
	loadState     string
	systemdResult string
	Registered    bool   `json:"registered"`
	Running       bool   `json:"running"`
	Enabled       bool   `json:"enabled,omitempty"`
	PID           int    `json:"pid,omitempty"`
	Label         string `json:"label"`
	Binary        string `json:"binary"`
}

func (m Manager) Binary() string { return filepath.Join(m.DataDir, "bin", "pagehub") }
func (m Manager) Plist() string {
	return filepath.Join(m.Home, "Library", "LaunchAgents", m.Settings.ServiceName+".plist")
}
func (m Manager) domain() string    { return "gui/" + strconv.Itoa(m.UID) }
func (m Manager) qualified() string { return m.domain() + "/" + m.Settings.ServiceName }
func (m Manager) platform() string {
	if m.GOOS != "" {
		return m.GOOS
	}
	return runtime.GOOS
}
func (m Manager) check() error {
	if m.platform() != "darwin" && m.platform() != "linux" {
		return errors.New("Pagehub supports macOS and Linux only")
	}
	return m.Settings.Validate()
}
func (m Manager) runner() Runner {
	if m.Run != nil {
		return m.Run
	}
	return Exec
}

var pidRE = regexp.MustCompile(`(?m)^\s*pid = ([0-9]+)`)

func (m Manager) Status(ctx context.Context) (Status, error) {
	s := Status{Label: m.Settings.ServiceName, Binary: m.Binary()}
	if err := m.check(); err != nil {
		return s, err
	}
	if m.platform() == "linux" {
		return m.systemdStatus(ctx)
	}
	out, err := m.runner()(ctx, "launchctl", "print", m.qualified())
	if err != nil {
		if strings.Contains(out, "Could not find service") || strings.Contains(out, "service not found") {
			return s, nil
		}
		return s, err
	}
	s.Registered = true
	s.Running = strings.Contains(out, "state = running")
	if match := pidRE.FindStringSubmatch(out); len(match) > 1 {
		s.PID, _ = strconv.Atoi(match[1])
	}
	return s, nil
}
func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func (m Manager) plist() []byte {
	args := []string{m.Binary(), "serve", "--data-dir", m.DataDir, "--port", strconv.Itoa(m.Settings.Port)}
	var b strings.Builder
	for _, a := range args {
		fmt.Fprintf(&b, "<string>%s</string>", xmlText(a))
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array>%s</array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>10</integer><key>ProcessType</key><string>Background</string><key>StandardOutPath</key><string>/dev/null</string><key>StandardErrorPath</key><string>/dev/null</string></dict></plist>
`, xmlText(m.Settings.ServiceName), b.String()))
}
func (m Manager) owned() error {
	if m.platform() == "linux" {
		return m.systemdOwned()
	}
	b, err := os.ReadFile(m.Plist())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), "<string>"+xmlText(m.Binary())+"</string>") {
		return errors.New("existing LaunchAgent belongs to another installation")
	}
	return nil
}
func (m Manager) Stop(ctx context.Context) error {
	if err := m.check(); err != nil {
		return err
	}
	if m.platform() == "linux" {
		return m.systemdStop(ctx)
	}
	if err := m.owned(); err != nil {
		return err
	}
	s, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if !s.Registered {
		return nil
	}
	if _, e := os.Stat(m.Plist()); e != nil {
		return errors.New("registered service has no owned LaunchAgent file; left unchanged")
	}
	_, err = m.runner()(ctx, "launchctl", "bootout", m.qualified())
	if err != nil {
		return err
	}
	alive := m.PIDAlive
	if alive == nil {
		alive = func(pid int) bool {
			if m.Run != nil || pid <= 0 {
				return false
			}
			p, e := os.FindProcess(pid)
			return e == nil && p.Signal(syscall.Signal(0)) == nil
		}
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		current, e := m.Status(ctx)
		if e != nil {
			return e
		}
		if !current.Registered && !alive(s.PID) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("service did not fully stop; not starting another process")
		case <-tick.C:
		}
	}
}
func (m Manager) Start(ctx context.Context) error {
	if err := m.check(); err != nil {
		return err
	}
	if m.platform() == "linux" {
		return m.systemdStart(ctx)
	}
	if err := m.owned(); err != nil {
		return err
	}
	if _, err := os.Stat(m.Plist()); err != nil {
		return errors.New("service is not installed; run pagehub setup")
	}
	s, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if s.Running {
		return nil
	}
	if s.Registered {
		_, err = m.runner()(ctx, "launchctl", "kickstart", m.qualified())
		return err
	}
	if _, err = m.runner()(ctx, "launchctl", "enable", m.qualified()); err != nil {
		return err
	}
	_, err = m.runner()(ctx, "launchctl", "bootstrap", m.domain(), m.Plist())
	return err
}
func (m Manager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	return m.Start(ctx)
}
func (m Manager) Wait(ctx context.Context) error {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/_health", m.Settings.Port)
	client, err := localhttp.NewClient(endpoint, "", time.Second)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		status, err := m.Status(ctx)
		if err == nil && status.Running && status.PID > 0 {
			req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
			resp, e := client.Do(req)
			if e == nil {
				var health struct {
					Service, Status, Version string
					PID                      int
				}
				decoder := json.NewDecoder(io.LimitReader(resp.Body, 4096))
				decodeErr := decoder.Decode(&health)
				var extra any
				endErr := decoder.Decode(&extra)
				resp.Body.Close()
				if decodeErr == nil && errors.Is(endErr, io.EOF) && resp.StatusCode == 200 &&
					health.Service == "pagehub" && health.Status == "ok" && health.Version != "" && health.PID == status.PID {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("background service did not become healthy; inspect pagehub.log")
		case <-tick.C:
		}
	}
}
func (m Manager) Install(ctx context.Context, source string) error {
	if err := m.check(); err != nil {
		return err
	}
	if err := m.owned(); err != nil {
		return err
	}
	b, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	paths := []string{m.Binary(), m.serviceFile(), filepath.Join(m.DataDir, "settings.json")}
	old := make([][]byte, len(paths))
	exists := make([]bool, len(paths))
	for i, path := range paths {
		old[i], err = os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		exists[i] = err == nil
	}
	oldStatus, err := m.Status(ctx)
	if err != nil {
		return err
	}
	// Migrate the stored label as well as the original legacy label. Snapshot
	// both jobs before mutation so a failed rename restores the prior service.
	stored, err := config.Load(m.DataDir)
	if err != nil {
		return err
	}
	type previousService struct {
		manager Manager
		content []byte
		status  Status
	}
	var previous []previousService
	var labels []string
	if exists[2] {
		labels = append(labels, stored.ServiceName)
	}
	if m.platform() == "darwin" && m.Settings.ServiceName == config.DefaultLabel && (!exists[2] || stored.ServiceName != config.LegacyLabel) {
		labels = append(labels, config.LegacyLabel)
	}
	for _, label := range labels {
		if label == m.Settings.ServiceName {
			continue
		}
		prior := m
		prior.Settings = stored
		prior.Settings.ServiceName = label
		content, e := os.ReadFile(prior.serviceFile())
		if errors.Is(e, os.ErrNotExist) {
			if label == stored.ServiceName {
				status, statusErr := prior.Status(ctx)
				if statusErr != nil {
					return statusErr
				}
				if status.Registered {
					return errors.New("previous service has no owned service file; left unchanged")
				}
			}
			continue
		}
		if e != nil {
			return e
		}
		if e = prior.owned(); e != nil {
			return e
		}
		status, e := prior.Status(ctx)
		if e != nil {
			return e
		}
		previous = append(previous, previousService{prior, content, status})
	}
	rollback := func(cause error) error {
		recovery, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		problems := []error{cause}
		if e := m.Stop(recovery); e != nil {
			// Never restore/start the old job while the replacement may still
			// own the data directory. Leave the replacement files consistent.
			return errors.Join(cause, fmt.Errorf("rollback could not stop replacement service; installation left in place: %w", e))
		}
		if m.platform() == "linux" {
			if e := m.systemdDisable(recovery); e != nil {
				return errors.Join(cause, e)
			}
		}
		for i, path := range paths {
			mode := os.FileMode(0600)
			if i == 0 {
				mode = 0700
			}
			var e error
			if exists[i] {
				e = config.AtomicWrite(path, old[i], mode)
			} else {
				e = os.Remove(path)
				if errors.Is(e, os.ErrNotExist) {
					e = nil
				}
			}
			if e != nil {
				problems = append(problems, e)
			}
		}
		if e := m.restoreService(recovery, oldStatus); e != nil {
			problems = append(problems, e)
		}
		for _, prior := range previous {
			if e := config.AtomicWrite(prior.manager.serviceFile(), prior.content, 0600); e != nil {
				problems = append(problems, e)
			}
			if e := prior.manager.restoreService(recovery, prior.status); e != nil {
				problems = append(problems, e)
			}
		}
		return errors.Join(problems...)
	}
	for _, prior := range previous {
		if err = prior.manager.Stop(ctx); err != nil {
			return rollback(err)
		}
	}
	if err = m.Stop(ctx); err != nil {
		return rollback(err)
	}
	if err = config.AtomicWrite(m.Binary(), b, 0700); err != nil {
		return rollback(err)
	}
	if err = config.AtomicWrite(m.serviceFile(), m.serviceContent(), 0600); err != nil {
		return rollback(err)
	}
	if err = config.Save(m.DataDir, m.Settings); err != nil {
		return rollback(err)
	}
	if err = m.Start(ctx); err != nil {
		return rollback(err)
	}
	if m.platform() == "linux" {
		if err = m.systemdEnable(ctx); err != nil {
			return rollback(err)
		}
	}
	ready := m.Ready
	if ready == nil {
		ready = m.Wait
	}
	if err = ready(ctx); err != nil {
		return rollback(err)
	}
	for _, prior := range previous {
		if prior.manager.platform() == "linux" {
			if err = prior.manager.systemdDisable(ctx); err != nil {
				return rollback(err)
			}
		}
		if err = os.Remove(prior.manager.serviceFile()); err != nil {
			return rollback(err)
		}
	}
	if m.platform() == "linux" {
		if err = m.systemdReload(ctx); err != nil {
			return rollback(err)
		}
	}
	return nil
}
func (m Manager) Uninstall(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	if m.platform() == "linux" {
		if err := m.systemdDisable(ctx); err != nil {
			return err
		}
	}
	if err := os.Remove(m.serviceFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if m.platform() == "linux" {
		if err := m.systemdReload(ctx); err != nil {
			return err
		}
	}
	if err := os.Remove(m.Binary()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
