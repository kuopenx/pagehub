package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeferredBackupCannotBlockUpdateOrResurrectDeletion(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user filesystem permissions")
	}
	for _, updateAgain := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "update_then_delete"}[updateAgain], func(t *testing.T) {
			dir := t.TempDir()
			store, err := OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			page, err := store.Create("regression", "<html>original</html>", "test-model / high")
			if err != nil {
				t.Fatal(err)
			}
			live := filepath.Join(store.dir, page.ID)
			backup := filepath.Join(store.dir, ".backup-"+page.ID)
			t.Cleanup(func() {
				entries, _ := os.ReadDir(store.dir)
				for _, entry := range entries {
					_ = os.Chmod(filepath.Join(store.dir, entry.Name()), 0700)
				}
			})
			if err = os.Chmod(live, 0500); err != nil {
				t.Fatal(err)
			}
			content := "<html>updated</html>"
			if _, err = store.Update(page.ID, nil, &content, "test-model / high"); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(backup); err != nil {
				t.Fatal("cleanup failure not exercised", err)
			}
			if updateAgain {
				content = "<html>updated again</html>"
				updated, err := store.Update(page.ID, nil, &content, "test-model / high")
				if err != nil || updated.Revision != 3 {
					t.Fatal("old backup blocked update", err)
				}
			}
			if err = store.Delete(page.ID); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenStore(dir)
			if err != nil {
				t.Fatal("obsolete cleanup blocked startup", err)
			}
			if reopened.Count() != 0 {
				t.Fatal("deleted page restored")
			}
			if _, _, _, _, err = reopened.Read(page.ID, 1, 0); !errors.Is(err, ErrNotFound) {
				t.Fatal("deleted page readable", err)
			}
		})
	}
}

func TestRecoveredObsoleteBackupCannotResurrectDeletion(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Create("regression", "<html>original</html>", "test-model / high")
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(store.dir, page.ID)
	backup := filepath.Join(store.dir, ".backup-"+page.ID)
	if err = os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "page.json"} {
		data, err := os.ReadFile(filepath.Join(live, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(backup, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Delete(page.ID); err != nil {
		t.Fatal(err)
	}
	again, err := OpenStore(dir)
	if err != nil || again.Count() != 0 {
		t.Fatal("recovered backup restored deleted page", err)
	}
}

func TestShutdownWaitsForActiveRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.Write([]byte("complete")) })}
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, srv, listener, time.Second) }()
	responseDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
		responseDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatal("returned before request completed", err)
	case <-time.After(100 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	if err := <-responseDone; err != nil {
		t.Fatal("request failed during drain", err)
	}
}

func TestShutdownTimeoutClosesConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	exited := make(chan struct{})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(exited) })}
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, srv, listener, 100*time.Millisecond) }()
	responseDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
		responseDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("expected drain timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown exceeded timeout")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("active connection not closed")
	}
	if err := <-responseDone; err == nil {
		t.Fatal("unfinished request unexpectedly succeeded")
	}
}
