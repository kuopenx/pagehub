package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testHTML = "<!doctype html><html><head><title>Test</title></head><body>hello</body></html>"

func TestStoreLifecycleAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Create("同名", testHTML)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Create("同名", testHTML)
	if err != nil || other.ID == p.ID {
		t.Fatalf("duplicate title must create unique IDs: %v", err)
	}
	newHTML, title := strings.ReplaceAll(testHTML, "hello", "updated"), "修改后的标题"
	u, err := s.Update(p.ID, &title, &newHTML)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != p.ID || !u.CreatedAt.Equal(p.CreatedAt) || !u.UpdatedAt.After(p.UpdatedAt) {
		t.Fatal("update did not preserve identity/creation time")
	}
	if len(s.List("修改后")) != 1 {
		t.Fatal("search failed")
	}
	f, _, err := s.Open(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != newHTML {
		t.Fatal("updated content mismatch")
	}
	// Simulate a crash between moving the current directory aside and publishing.
	if err := os.Rename(filepath.Join(s.dir, p.ID), filepath.Join(s.dir, ".backup-"+p.ID)); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(s.dir, ".stage-interrupted")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil || reopened.Count() != 2 {
		t.Fatalf("restart/recovery failed: %v", err)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging files retained")
	}
	if err := reopened.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.Open(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted page still available")
	}
	if _, err := os.Stat(filepath.Join(s.dir, p.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted file still exists")
	}
	if len(reopened.List("修改后")) != 0 {
		t.Fatal("deleted metadata remains")
	}
	restarted, err := OpenStore(dir)
	if err != nil || restarted.Count() != 1 {
		t.Fatalf("deletion not persisted: %v", err)
	}
}

func TestStoreConcurrentReadUpdate(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Create("concurrent", testHTML)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				f, _, err := s.Open(p.ID)
				if err != nil {
					t.Error(err)
					return
				}
				b, err := io.ReadAll(f)
				f.Close()
				if err != nil || !strings.Contains(string(b), "</html>") {
					t.Error("partial page read")
				}
				_ = s.List("")
			}
		})
	}
	wg.Go(func() {
		for i := 0; i < 10; i++ {
			html := strings.ReplaceAll(testHTML, "hello", strings.Repeat("updated", i+1))
			if _, err := s.Update(p.ID, nil, &html); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()
}

func TestValidationAndNoPageCountLimit(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, html := range []string{"", "/tmp/page.html", "https://example.com", "<div>fragment</div>"} {
		if _, err := s.Create("bad", html); err == nil {
			t.Errorf("accepted unsupported content %q", html)
		}
	}
	if _, err := s.Create(" ", testHTML); err == nil {
		t.Fatal("blank title accepted")
	}
	for i := 0; i < 12; i++ {
		if _, err := s.Create("same name", testHTML); err != nil {
			t.Fatal(err)
		}
	}
	if s.Count() != 12 {
		t.Fatal("unexpected page limit")
	}
	if err := s.Delete("../../token"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
