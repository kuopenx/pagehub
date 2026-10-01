package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuopenx/pagehub/internal/buildinfo"
)

func TestVersionIgnoresBrokenOrInaccessibleSettings(t *testing.T) {
	for _, settings := range []string{"{", `{"schema_version":99}`, `{"port":0}`} {
		t.Run(settings, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0600); err != nil {
				t.Fatal(err)
			}
			for _, format := range []string{"text", "json"} {
				args := []string{"version", "--data-dir", dir}
				if format == "json" {
					args = append(args, "--json")
				}
				var out, stderr bytes.Buffer
				if code := Run(context.Background(), args, &out, &stderr, Environment{Home: dir}); code != 0 {
					t.Fatalf("exit=%d %s", code, stderr.String())
				}
				if !strings.Contains(out.String(), buildinfo.Version) {
					t.Fatal("version missing")
				}
				if format == "json" {
					var r result
					if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Version != buildinfo.Version {
						t.Fatal("invalid version JSON", err)
					}
				}
			}
		})
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "settings.json"), 0700); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Run(context.Background(), []string{"version", "--data-dir", dir}, &out, &stderr, Environment{Home: dir}) != 0 {
		t.Fatal("version tried to read settings directory", stderr.String())
	}
}
