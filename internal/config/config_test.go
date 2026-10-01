package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsLifecycleAndValidation(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil || s.Port != 8765 {
		t.Fatal(err)
	}
	s.Port = 9876
	if err = Save(dir, s); err != nil {
		t.Fatal(err)
	}
	again, err := Load(dir)
	if err != nil || again != s {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "settings.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
	for _, bad := range []Settings{{1, 0, DefaultLabel}, {1, 70000, DefaultLabel}, {2, 8765, DefaultLabel}, {1, 8765, "../foreign"}} {
		if err = Save(dir, bad); err == nil {
			t.Fatal("invalid settings accepted")
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{"), 0600)
	if _, err = Load(dir); err == nil {
		t.Fatal("malformed settings accepted")
	}
}
