package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReadPatchAtomicityAndRevision(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	source := "<html>\r\n" + strings.Repeat("鹈", 30000) + "\r\nhello\r\n</html>\r\n"
	p, err := s.Create("test", source)
	if err != nil {
		t.Fatal(err)
	}
	_, part, end, total, err := s.Read(p.ID, 2, 2)
	if err != nil || part != strings.Repeat("鹈", 30000)+"\r\n" || end != 2 || total != 4 {
		t.Fatalf("long exact line read: %v", err)
	}
	_, part, end, _, err = s.Read(p.ID, 3, 99)
	if err != nil || part != "hello\r\n</html>\r\n" || end != 4 {
		t.Fatal("EOF clamp")
	}
	if _, _, _, _, err = s.Read(p.ID, 5, 0); err == nil {
		t.Fatal("invalid range accepted")
	}
	failed := [][]TextEdit{
		{{"hello", "changed"}, {"missing", "x"}},
		{{"", "x"}},
		{{"<html>", "<fragment>"}},
	}
	for _, edits := range failed {
		if _, err := s.Patch(p.ID, 1, edits); err == nil {
			t.Fatal("invalid patch accepted")
		}
		current, text, _, _, _ := s.Read(p.ID, 1, 0)
		if text != source || current != p {
			t.Fatal("failed patch modified page")
		}
	}
	u, err := s.Patch(p.ID, 1, []TextEdit{{"hello", "changed"}, {"changed", "done"}})
	if err != nil || u.Revision != 2 || u.CreatedAt != p.CreatedAt {
		t.Fatalf("patch: %v", err)
	}
	if _, err := s.Patch(p.ID, 1, []TextEdit{{"done", "stale"}}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale revision accepted")
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, text, _, _, err := s.Read(p.ID, 1, 0)
	if err != nil || u.Revision != 2 || !strings.Contains(text, "done") {
		t.Fatal("revision/content not persisted")
	}
	// Simulate legacy metadata without a revision field.
	meta := filepath.Join(s.dir, p.ID, "page.json")
	data, _ := os.ReadFile(meta)
	data = []byte(strings.Replace(string(data), `"revision": 2`, `"legacy": true`, 1))
	if err := os.WriteFile(meta, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil || s.List("")[0].Revision != 1 {
		t.Fatal("legacy revision migration")
	}
}

func TestPatchUniqueOverlapAndConcurrentWriters(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	p, _ := s.Create("overlap", "<html>aaaa</html>")
	if _, err := s.Patch(p.ID, 1, []TextEdit{{"aaa", "x"}}); err == nil {
		t.Fatal("overlapping duplicate match accepted")
	}
	p, _ = s.Create("concurrent", "<html>original</html>")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, next := range []string{"writerA", "writerB"} {
		wg.Go(func() { _, err := s.Patch(p.ID, 1, []TextEdit{{"original", next}}); results <- err })
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent edits did not reject stale writer")
	}
	newTitle := "renamed"
	u, err := s.Update(p.ID, &newTitle, nil)
	if err != nil || u.Revision != 3 {
		t.Fatal("update did not increment revision")
	}
	expected := int64(2)
	if _, err := s.UpdateChecked(p.ID, &newTitle, nil, &expected); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("checked update accepted stale revision")
	}
}
