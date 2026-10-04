package server

import (
	"context"
	"errors"
	"fmt"
	"github.com/kuopenx/pagehub/internal/buildinfo"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	tokenPath := filepath.Join(dataDir, "token")
	_, err = LoadToken(tokenPath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: newApp(store, func(candidate string) bool { return AcceptToken(tokenPath, candidate) }, port), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf("pagehub %s ready on port %d; %d pages loaded", version, port, store.Count())
	return serveHTTP(ctx, srv, listener, 5*time.Second)
}

func serveHTTP(ctx context.Context, srv *http.Server, listener net.Listener, grace time.Duration) error {
	serveDone := make(chan struct{})
	shutdownDone := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), grace)
			defer cancel()
			err := srv.Shutdown(shutdown)
			if err != nil {
				_ = srv.Close()
			}
			shutdownDone <- err
		case <-serveDone:
			shutdownDone <- nil
		}
	}()
	err := srv.Serve(listener)
	close(serveDone)
	shutdownErr := <-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return shutdownErr
	}
	return err
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
