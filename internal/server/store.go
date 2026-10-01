package server

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var htmlRoot = regexp.MustCompile(`(?i)<html(?:\s|>)`)
var ErrNotFound = errors.New("page not found")
var ErrRevisionConflict = errors.New("revision conflict")

type Page struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	MediaType string    `json:"media_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SizeBytes int64     `json:"size_bytes"`
	Revision  int64     `json:"revision"`
}

type Store struct {
	mu    sync.RWMutex
	dir   string
	pages map[string]Page
}

// Each page's page.json is the persistent catalog entry, so no growing global
// manifest needs to be rewritten. Only metadata is indexed in RAM.
func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, "pages")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, pages: make(map[string]Page)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// Recover an interrupted update, or finish removal of obsolete generations.
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		if strings.HasPrefix(name, ".backup-") {
			id := strings.TrimPrefix(name, ".backup-")
			if !idPattern.MatchString(id) {
				return nil, fmt.Errorf("invalid recovery entry")
			}
			if _, err := os.Stat(filepath.Join(dir, id)); errors.Is(err, os.ErrNotExist) {
				if err := os.Rename(path, filepath.Join(dir, id)); err != nil {
					return nil, err
				}
			} else if err == nil {
				if err := s.retireBackup(id); err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else if strings.HasPrefix(name, ".stage-") || strings.HasPrefix(name, ".trash-") {
			if err := os.RemoveAll(path); err != nil {
				log.Printf("obsolete page generation cleanup deferred: %v", err)
			}
		}
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !idPattern.MatchString(entry.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name(), "page.json"))
		if err != nil {
			return nil, fmt.Errorf("load page %s: %w", entry.Name(), err)
		}
		var p Page
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		if p.ID != entry.Name() || p.MediaType != "text/html" {
			return nil, fmt.Errorf("invalid page metadata")
		}
		info, err := os.Stat(filepath.Join(dir, p.ID, "index.html"))
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("missing page content for %s", p.ID)
		}
		p.SizeBytes = info.Size()
		if p.Revision < 1 {
			p.Revision = 1 // Legacy pages start at revision 1.
		}
		s.pages[p.ID] = p
	}
	return s, nil
}

func uuid() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func validateHTML(content string) error {
	if !utf8.ValidString(content) || strings.TrimSpace(content) == "" {
		return errors.New("html must be non-empty UTF-8 text")
	}
	if !htmlRoot.MatchString(content) {
		return errors.New("submit a complete HTML document containing an <html> element; fragments, file paths and URLs are not accepted")
	}
	return nil
}

func (s *Store) Create(title, content string) (Page, error) {
	if strings.TrimSpace(title) == "" {
		return Page{}, errors.New("title must not be blank")
	}
	if err := validateHTML(content); err != nil {
		return Page{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	for {
		var err error
		id, err = uuid()
		if err != nil {
			return Page{}, err
		}
		if _, exists := s.pages[id]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.dir, id)); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return Page{}, err
		}
	}
	now := time.Now().UTC()
	p := Page{ID: id, Title: strings.TrimSpace(title), MediaType: "text/html", CreatedAt: now, UpdatedAt: now, Revision: 1}
	if err := s.commit(&p, strings.NewReader(content), false); err != nil {
		return Page{}, err
	}
	s.pages[id] = p
	return p, nil
}

func (s *Store) Update(id string, title, content *string) (Page, error) {
	return s.UpdateChecked(id, title, content, nil)
}

func (s *Store) UpdateChecked(id string, title, content *string, expected *int64) (Page, error) {
	if !idPattern.MatchString(id) {
		return Page{}, errors.New("id must be a server-generated UUID")
	}
	if title == nil && content == nil {
		return Page{}, errors.New("provide title or html to update")
	}
	if title != nil && strings.TrimSpace(*title) == "" {
		return Page{}, errors.New("title must not be blank")
	}
	if content != nil {
		if err := validateHTML(*content); err != nil {
			return Page{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, exists := s.pages[id]
	if !exists {
		return Page{}, ErrNotFound
	}
	if expected != nil && *expected != p.Revision {
		return Page{}, fmt.Errorf("%w: expected %d, current %d; read_page again", ErrRevisionConflict, *expected, p.Revision)
	}
	if title != nil {
		p.Title = strings.TrimSpace(*title)
	}
	p.UpdatedAt = time.Now().UTC()
	p.Revision++
	var reader io.Reader
	if content != nil {
		reader = strings.NewReader(*content)
	} else {
		f, err := os.Open(filepath.Join(s.dir, id, "index.html"))
		if err != nil {
			return Page{}, err
		}
		defer f.Close()
		reader = f
	}
	if err := s.commit(&p, reader, true); err != nil {
		return Page{}, err
	}
	s.pages[id] = p
	return p, nil
}

type TextEdit struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// Read returns exact source lines, including their original newline characters.
// Open captures the metadata and file together; updates cannot mix generations.
func (s *Store) Read(id string, start, end int) (Page, string, int, int, error) {
	if start < 1 || end < 0 || (end != 0 && end < start) {
		return Page{}, "", 0, 0, errors.New("invalid line range")
	}
	f, p, err := s.Open(id)
	if err != nil {
		return Page{}, "", 0, 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var out strings.Builder
	total, last := 0, 0
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			total++
			if total >= start && (end == 0 || total <= end) {
				out.WriteString(line)
				last = total
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Page{}, "", 0, 0, err
		}
	}
	if start > total {
		return Page{}, "", 0, 0, fmt.Errorf("start_line %d exceeds total_lines %d", start, total)
	}
	return p, out.String(), last, total, nil
}

// All edits and the revision check run under one lock. Failed batches publish nothing.
func (s *Store) Patch(id string, expected int64, edits []TextEdit) (Page, error) {
	if !idPattern.MatchString(id) || expected < 1 || len(edits) == 0 {
		return Page{}, errors.New("patch requires a valid UUID, expected_revision >= 1 and at least one edit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[id]
	if !ok {
		return Page{}, ErrNotFound
	}
	if expected != p.Revision {
		return Page{}, fmt.Errorf("%w: expected %d, current %d; read_page again", ErrRevisionConflict, expected, p.Revision)
	}
	b, err := os.ReadFile(filepath.Join(s.dir, id, "index.html"))
	if err != nil {
		return Page{}, err
	}
	content := string(b)
	for i, edit := range edits {
		if edit.OldText == "" {
			return Page{}, fmt.Errorf("edit %d: old_text must not be empty", i+1)
		}
		at := strings.Index(content, edit.OldText)
		if at < 0 {
			return Page{}, fmt.Errorf("edit %d: old_text was not found", i+1)
		}
		if strings.Contains(content[at+1:], edit.OldText) {
			return Page{}, fmt.Errorf("edit %d: old_text is not unique; include more surrounding text", i+1)
		}
		content = content[:at] + edit.NewText + content[at+len(edit.OldText):]
	}
	if err := validateHTML(content); err != nil {
		return Page{}, err
	}
	p.Revision++
	p.UpdatedAt = time.Now().UTC()
	if err := s.commit(&p, strings.NewReader(content), true); err != nil {
		return Page{}, err
	}
	s.pages[id] = p
	return p, nil
}

func (s *Store) commit(p *Page, reader io.Reader, replace bool) error {
	stage, err := os.MkdirTemp(s.dir, ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	f, err := os.OpenFile(filepath.Join(stage, "index.html"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	p.SizeBytes, err = io.Copy(f, reader)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	meta, err := os.OpenFile(filepath.Join(stage, "page.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = meta.Write(b)
	if err == nil {
		err = meta.Sync()
	}
	closeErr = meta.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	live := filepath.Join(s.dir, p.ID)
	backup := filepath.Join(s.dir, ".backup-"+p.ID)
	if replace {
		if err := s.retireBackup(p.ID); err != nil {
			return err
		}
		if err := os.Rename(live, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, live); err != nil {
		if replace {
			if restoreErr := os.Rename(backup, live); restoreErr != nil {
				return fmt.Errorf("commit failed (%v), restore failed (%v)", err, restoreErr)
			}
		}
		return err
	}
	if replace {
		if err := os.RemoveAll(backup); err != nil {
			// Publication succeeded. Keep the index consistent and let startup retry cleanup.
			log.Printf("page %s saved; backup cleanup deferred: %v", p.ID, err)
		}
	}
	return nil
}

// Once a live generation exists, its previous backup must never be recovered.
// Rename before attempting cleanup: even an undeletable generation is now
// unambiguously obsolete, across later updates, deletion and process restarts.
func (s *Store) retireBackup(id string) error {
	backup := filepath.Join(s.dir, ".backup-"+id)
	if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	suffix, err := uuid()
	if err != nil {
		return err
	}
	trash := filepath.Join(s.dir, ".trash-"+suffix)
	if err := os.Rename(backup, trash); err != nil {
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		log.Printf("obsolete page generation cleanup deferred: %v", err)
	}
	return nil
}

func (s *Store) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return errors.New("id must be a server-generated UUID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pages[id]; !exists {
		return ErrNotFound
	}
	if err := s.retireBackup(id); err != nil {
		return err
	}
	live := filepath.Join(s.dir, id)
	trash := filepath.Join(s.dir, ".trash-"+id)
	if err := os.Rename(live, trash); err != nil {
		return err
	}
	delete(s.pages, id)
	if err := os.RemoveAll(trash); err != nil {
		return fmt.Errorf("page unlisted, file cleanup incomplete: %w", err)
	}
	return nil
}

func (s *Store) List(query string) []Page {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query = strings.ToLower(strings.TrimSpace(query))
	pages := make([]Page, 0, len(s.pages))
	for _, p := range s.pages {
		if query == "" || strings.Contains(strings.ToLower(p.Title), query) || strings.Contains(p.ID, query) {
			pages = append(pages, p)
		}
	}
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].CreatedAt.Equal(pages[j].CreatedAt) {
			return pages[i].ID > pages[j].ID
		}
		return pages[i].CreatedAt.After(pages[j].CreatedAt)
	})
	return pages
}

func (s *Store) Count() int { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.pages) }

func (s *Store) Open(id string) (*os.File, Page, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, exists := s.pages[id]
	if !exists {
		return nil, Page{}, ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.dir, id, "index.html"))
	return f, p, err
}
