package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const DefaultLabel = "io.pagehub.agent"
const LegacyLabel = "com.garyshu.pagehub"

type Settings struct {
	SchemaVersion int    `json:"schema_version"`
	Port          int    `json:"port"`
	ServiceName   string `json:"service_name"`
}

var labelPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{2,100}$`)

func Default() Settings { return Settings{SchemaVersion: 1, Port: 8765, ServiceName: DefaultLabel} }
func (s Settings) Validate() error {
	if s.SchemaVersion != 1 {
		return errors.New("unsupported settings schema")
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("port must be 1..65535")
	}
	if !labelPattern.MatchString(s.ServiceName) {
		return errors.New("invalid service name")
	}
	return nil
}
func Load(dir string) (Settings, error) {
	s := Default()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, s.Validate()
}
func Save(dir string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(dir, "settings.json"), append(b, '\n'), 0600)
}
func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pagehub-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
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
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	return nil
}
