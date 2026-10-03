package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAttributionPersistenceAndAtomicity(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Create("attribution", testHTML, " creator-model / high ")
	if err != nil || p.CreatedBy != "creator-model / high" || p.UpdatedBy != p.CreatedBy {
		t.Fatal(p, err)
	}
	title := "renamed"
	u, err := s.Update(p.ID, &title, nil, "update-model / low")
	if err != nil || u.CreatedBy != p.CreatedBy || u.UpdatedBy != "update-model / low" {
		t.Fatal(u, err)
	}
	u, err = s.Patch(p.ID, 2, []TextEdit{{"hello", "patched"}}, "patch-model / medium")
	if err != nil || u.CreatedBy != p.CreatedBy || u.UpdatedBy != "patch-model / medium" || u.Revision != 3 {
		t.Fatal(u, err)
	}
	metadata := filepath.Join(s.dir, p.ID, "page.json")
	before, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Patch(p.ID, 3, []TextEdit{{"patched", "changed"}, {"missing", "bad"}}, "failed-model / high"); err == nil {
		t.Fatal("failed batch accepted")
	}
	stale := int64(2)
	if _, err = s.UpdateChecked(p.ID, &title, nil, &stale, "stale-model / high"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(metadata)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed changes mutated metadata", err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, content, _, _, err := reopened.Read(p.ID, 1, 0)
	if err != nil || saved != u || content != "<!doctype html><html><head><title>Test</title></head><body>patched</body></html>" {
		t.Fatal("attribution/content not persisted", saved, err)
	}
	for _, bad := range []string{"", " ", "model-only", "model / ", "model\nname / high", "model / high\x00", "\xff / high"} {
		if _, err = s.Create("bad", testHTML, bad); err == nil {
			t.Fatalf("accepted attribution %q", bad)
		}
		if _, err = s.Update(p.ID, &title, nil, bad); err == nil {
			t.Fatalf("updated attribution %q", bad)
		}
		if _, err = s.Patch(p.ID, 3, []TextEdit{{"patched", "bad"}}, bad); err == nil {
			t.Fatalf("patched attribution %q", bad)
		}
	}
}

func TestLegacyAttributionRemainsUnknown(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Create("legacy", testHTML, "fixture-model / high")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, p.ID, "page.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err = json.Unmarshal(b, &metadata); err != nil {
		t.Fatal(err)
	}
	delete(metadata, "created_by")
	delete(metadata, "updated_by")
	b, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy, _, _, _, err := s.Read(p.ID, 1, 0)
	if err != nil || legacy.CreatedBy != "" || legacy.UpdatedBy != "" || legacy.Revision != 1 {
		t.Fatal(legacy, err)
	}
	title := "updated legacy"
	updated, err := s.Update(p.ID, &title, nil, "current-model / high")
	if err != nil || updated.CreatedBy != "" || updated.UpdatedBy != "current-model / high" || updated.CreatedAt != p.CreatedAt {
		t.Fatal(updated, err)
	}
}
