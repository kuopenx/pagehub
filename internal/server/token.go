package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kuopenx/pagehub/internal/config"
)

const revokedToken = "revoked\n"

// ReadToken never creates credentials. An explicitly revoked token is empty;
// missing or malformed files are errors and authentication fails closed.
func ReadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if string(b) == revokedToken {
		return "", nil
	}
	t := strings.TrimSpace(string(b))
	if len(t) != 64 {
		return "", fmt.Errorf("invalid management token file")
	}
	if _, err := hex.DecodeString(t); err != nil {
		return "", fmt.Errorf("invalid management token file")
	}
	return t, nil
}

// LoadToken creates a credential only on first installation. A revoked token
// remains revoked across setup and process restarts.
func LoadToken(path string) (string, error) { return changeToken(path, "initialize") }

// GenerateToken reuses an active token; missing or revoked credentials get a
// fresh value. RotateToken always replaces the credential.
func GenerateToken(path string) (string, error) { return changeToken(path, "generate") }
func RotateToken(path string) (string, error)   { return changeToken(path, "rotate") }
func RevokeToken(path string) error {
	_, err := changeToken(path, "revoke")
	return err
}

func changeToken(path, action string) (string, error) {
	return withTokenLock(path, func() (string, error) {
		if action == "initialize" || action == "generate" {
			t, err := ReadToken(path)
			if err == nil && (t != "" || action == "initialize") {
				return t, nil
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		if action == "revoke" {
			return "", config.AtomicWrite(path, []byte(revokedToken), 0600)
		}
		t, err := randomToken()
		if err != nil {
			return "", err
		}
		if err := config.AtomicWrite(path, []byte(t+"\n"), 0600); err != nil {
			return "", err
		}
		return t, nil
	})
}

func randomToken() (string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func withTokenLock(path string, fn func() (string, error)) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	// Serialize CLI commands and startup across processes. Atomic rename keeps
	// readers from seeing a partially written token. Keep the lock file in place
	// so concurrent commands always lock the same inode.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}
