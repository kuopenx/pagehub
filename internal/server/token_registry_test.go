package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestNamedTokensIndependentAndLegacyCompatible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	legacy, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := ChangeNamedToken(path, "laptop", "generate")
	if err != nil {
		t.Fatal(err)
	}
	phone, err := ChangeNamedToken(path, "phone", "generate")
	if err != nil || len(phone) != 64 || phone == laptop || laptop == legacy || phone == legacy {
		t.Fatal("device credentials not independent", err)
	}
	if repeat, err := ChangeNamedToken(path, "phone", "generate"); err != nil || repeat != phone {
		t.Fatal("repeated generate changed active device credential", err)
	}
	for _, token := range []string{legacy, laptop, phone} {
		if !AcceptToken(path, token) {
			t.Fatal("valid credential rejected")
		}
	}
	if _, err := ChangeNamedToken(path, "phone", "revoke"); err != nil {
		t.Fatal(err)
	}
	if AcceptToken(path, phone) || !AcceptToken(path, laptop) || !AcceptToken(path, legacy) {
		t.Fatal("device revocation affected unrelated credentials")
	}
	revoked, err := os.ReadFile(registryPath(path))
	if err != nil || bytes.Contains(revoked, []byte(phone)) {
		t.Fatal("revoked secret retained", err)
	}
	if _, err := ChangeNamedToken(path, "phone", "revoke"); err != nil {
		t.Fatal(err)
	}
	repeated, _ := os.ReadFile(registryPath(path))
	if !bytes.Equal(revoked, repeated) {
		t.Fatal("repeat revocation rewrote history")
	}
	if _, err := LoadToken(path); err != nil || AcceptToken(path, phone) || !AcceptToken(path, laptop) {
		t.Fatal("startup changed device revocation", err)
	}
	info, err := ListTokens(path)
	b, marshalErr := json.Marshal(info)
	if err != nil || marshalErr != nil || len(info) != 3 || bytes.Contains(b, []byte(legacy)) || bytes.Contains(b, []byte(laptop)) || bytes.Contains(b, []byte(phone)) {
		t.Fatal("listing exposed credentials or lost entries", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("named commands rewrote the original single token")
	}
	fileInfo, err := os.Stat(registryPath(path))
	if err != nil || fileInfo.Mode().Perm() != 0600 {
		t.Fatal("registry not private", err)
	}
	rotated, err := ChangeNamedToken(path, "laptop", "rotate")
	if err != nil || rotated == laptop || AcceptToken(path, laptop) || !AcceptToken(path, rotated) || !AcceptToken(path, legacy) {
		t.Fatal("device rotation affected unrelated credentials", err)
	}
	fresh, err := ChangeNamedToken(path, "phone", "generate")
	if err != nil || fresh == phone || !AcceptToken(path, fresh) || AcceptToken(path, phone) {
		t.Fatal("device regeneration reused revoked value", err)
	}
	if err := RevokeToken(path); err != nil || AcceptToken(path, legacy) || !AcceptToken(path, rotated) || !AcceptToken(path, fresh) {
		t.Fatal("default revocation disabled device tokens", err)
	}
	if _, err := LoadToken(path); err != nil || AcceptToken(path, legacy) || !AcceptToken(path, fresh) {
		t.Fatal("startup restored default or disabled device credentials", err)
	}
}

func TestConcurrentDeviceTokenChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	const count = 16
	tokens, errs := make([]string, count), make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() { tokens[i], errs[i] = ChangeNamedToken(path, fmt.Sprintf("device-%d", i), "generate") })
	}
	wg.Wait()
	for i := range count {
		if errs[i] != nil || !AcceptToken(path, tokens[i]) {
			t.Fatal("concurrent device creation lost credentials", errs[i])
		}
		wg.Go(func() { _, errs[i] = ChangeNamedToken(path, fmt.Sprintf("device-%d", i), "revoke") })
	}
	wg.Wait()
	infos, err := ListTokens(path)
	if err != nil || len(infos) != count+1 {
		t.Fatal("concurrent changes lost entries", err)
	}
	for i := range count {
		if errs[i] != nil || AcceptToken(path, tokens[i]) {
			t.Fatal("concurrent revocation lost a write", errs[i])
		}
	}
}

func TestFailedNamedTokenWritePreservesRegistry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user filesystem permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	token, err := ChangeNamedToken(path, "phone", "generate")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(registryPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	for _, action := range []string{"rotate", "revoke"} {
		if _, err := ChangeNamedToken(path, "phone", action); err == nil {
			t.Fatal("write unexpectedly succeeded in a read-only directory")
		}
	}
	after, _ := os.ReadFile(registryPath(path))
	if !bytes.Equal(before, after) || !AcceptToken(path, token) {
		t.Fatal("failed write invalidated the existing device credential")
	}
}

func TestMalformedRegistryCannotGrantAccessOrOverwriteCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	legacy, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	device, err := ChangeNamedToken(path, "phone", "generate")
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"{", `{}`, `{"tokens":[]}`, `{"schema_version":1}`, `{"schema_version":2,"tokens":[]}`, `{"schema_version":1,"tokens":null}`, `{"schema_version":1,"tokens":[],"unknown":true}`, `{"schema_version":1,"tokens":[]} {}`} {
		if err := os.WriteFile(registryPath(path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if AcceptToken(path, device) || !AcceptToken(path, legacy) {
			t.Fatal("malformed registry granted device access or blocked legacy credential")
		}
		for _, action := range []string{"generate", "rotate", "revoke"} {
			if _, err := ChangeNamedToken(path, "phone", action); err == nil {
				t.Fatal("malformed registry silently overwritten")
			}
		}
		b, _ := os.ReadFile(registryPath(path))
		if string(b) != content {
			t.Fatal("malformed registry changed")
		}
	}
}

func TestLiveIndependentDeviceRevocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	legacy, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := ChangeNamedToken(path, "phone", "generate")
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := ChangeNamedToken(path, "laptop", "generate")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
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
		r.Host = "personal-pagehub.example"
		r.Header.Set("Origin", "https://my-agent.example")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		response, err := h.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("status %d want %d", response.StatusCode, status)
		}
	}
	check(legacy, 200)
	check(phone, 200)
	check(laptop, 200)
	if _, err := ChangeNamedToken(path, "phone", "revoke"); err != nil {
		t.Fatal(err)
	}
	check(phone, 401)
	check(laptop, 200)
	check(legacy, 200)
	if err := RevokeToken(path); err != nil {
		t.Fatal(err)
	}
	check(legacy, 401)
	check(laptop, 200)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check(laptop, 200)
}
