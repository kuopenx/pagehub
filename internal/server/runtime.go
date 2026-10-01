package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/kuopenx/pagehub/internal/buildinfo"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var version = buildinfo.Version

func Serve(ctx context.Context, dataDir string, port int) error {
	store, err := OpenStore(dataDir)
	if err != nil {
		return err
	}
	logger := &rotatingLog{path: filepath.Join(dataDir, "pagehub.log")}
	defer logger.Close()
	log.SetOutput(logger)
	token, err := LoadToken(filepath.Join(dataDir, "token"))
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: NewApp(store, token, port), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		case <-done:
		}
	}()
	log.Printf("pagehub %s ready on port %d; %d pages loaded", version, port, store.Count())
	err = srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func LoadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		t := strings.TrimSpace(string(b))
		if len(t) != 64 {
			return "", fmt.Errorf("invalid management token file")
		}
		if _, err := hex.DecodeString(t); err != nil {
			return "", fmt.Errorf("invalid management token file")
		}
		return t, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	t := hex.EncodeToString(random[:])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = io.WriteString(f, t+"\n")
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return t, closeErr
}

// Keep at most two small log files. No access logs or submitted HTML are logged.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		if info, err := l.f.Stat(); err == nil && info.Size()+int64(len(p)) > 1<<20 {
			_ = l.f.Close()
			l.f = nil
			if err := os.Rename(l.path, l.path+".1"); err != nil {
				return 0, err
			}
		}
	}
	if l.f == nil {
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return 0, err
		}
		l.f = f
	}
	return l.f.Write(p)
}

func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}
