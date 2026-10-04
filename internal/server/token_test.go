package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTokenLifecycleAndConcurrentInitialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	const count = 16
	tokens, errs := make([]string, count), make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() { tokens[i], errs[i] = LoadToken(path) })
	}
	wg.Wait()
	for i := range count {
		if errs[i] != nil || len(tokens[i]) != 64 || tokens[i] != tokens[0] {
			t.Fatal("concurrent startup did not reuse one credential", errs[i])
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("token permissions", err)
	}
	if token, err := GenerateToken(path); err != nil || token != tokens[0] {
		t.Fatal("repeated generate replaced active token", err)
	}
	rotated, err := RotateToken(path)
	if err != nil || rotated == tokens[0] || len(rotated) != 64 {
		t.Fatal("rotation did not replace token", err)
	}
	for range 2 {
		if err := RevokeToken(path); err != nil {
			t.Fatal(err)
		}
		if token, err := LoadToken(path); err != nil || token != "" {
			t.Fatal("startup resurrected a revoked token", err)
		}
	}
	regenerated, err := GenerateToken(path)
	if err != nil || regenerated == rotated || regenerated == tokens[0] || len(regenerated) != 64 {
		t.Fatal("generation after revocation did not produce a fresh token", err)
	}
	if token, err := ReadToken(path); err != nil || token != regenerated {
		t.Fatal("token did not persist", err)
	}
}

func TestInvalidTokenNeverSilentlyReplaced(t *testing.T) {
	for _, value := range []string{"", "broken", strings.Repeat("z", 64), "revoked"} {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadToken(path); err == nil {
			t.Fatal("invalid token accepted")
		}
		if _, err := GenerateToken(path); err == nil {
			t.Fatal("invalid token silently replaced")
		}
		b, err := os.ReadFile(path)
		if err != nil || string(b) != value {
			t.Fatal("invalid token file changed", err)
		}
	}
}

func TestFailedTokenWritePreservesCredential(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user filesystem permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	original, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if _, err := RotateToken(path); err == nil {
		t.Fatal("rotation unexpectedly succeeded in read-only directory")
	}
	if err := RevokeToken(path); err == nil {
		t.Fatal("revocation unexpectedly succeeded in read-only directory")
	}
	if current, err := ReadToken(path); err != nil || current != original {
		t.Fatal("failed write changed credential", err)
	}
}

func TestLiveTokenRotationAndRevocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	old, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.Create("preserved", testHTML, "test-model / high")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(filepath.Join(dir, "pages", page.ID, "page.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(newApp(store, func(candidate string) bool { return AcceptToken(path, candidate) }, 8765))
	defer h.Close()
	check := func(token string, status int) {
		t.Helper()
		r, err := http.NewRequest("POST", h.URL+"/_mcp", strings.NewReader(mcpInitializeRequest))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := h.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("MCP status %d want %d", resp.StatusCode, status)
		}
	}
	check(old, http.StatusOK)
	newToken, err := RotateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	check(old, http.StatusUnauthorized)
	check(newToken, http.StatusOK)
	if err := RevokeToken(path); err != nil {
		t.Fatal(err)
	}
	check(newToken, http.StatusUnauthorized)
	check("revoked", http.StatusUnauthorized)
	if token, err := LoadToken(path); err != nil || token != "" {
		t.Fatal("startup re-enabled MCP", err)
	}
	check(newToken, http.StatusUnauthorized)
	newToken, err = GenerateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	check(newToken, http.StatusOK)
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	check(newToken, http.StatusUnauthorized)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check(newToken, http.StatusUnauthorized)
	resp, err := h.Client().Get(h.URL + "/" + page.ID + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || string(content) != testHTML {
		t.Fatal("revocation affected page access", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "pages", page.ID, "page.json"))
	if err != nil || !bytes.Equal(metadata, after) {
		t.Fatal("token maintenance changed page metadata", err)
	}
}
