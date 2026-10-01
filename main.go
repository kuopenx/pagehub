package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const version = "0.2.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("pagehub", version)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := flag.String("data-dir", filepath.Join(home, ".pagehub"), "Private page storage directory")
	port := flag.Int("port", 8765, "Shared HTTP and MCP port")
	flag.Parse()
	if *port < 1 || *port > 65535 {
		log.Fatal("invalid port")
	}
	store, err := OpenStore(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	logger := &rotatingLog{path: filepath.Join(*dataDir, "pagehub.log")}
	defer logger.Close()
	log.SetOutput(logger)
	token, err := loadToken(filepath.Join(*dataDir, "token"))
	if err != nil {
		log.Fatal(err)
	}
	// All mutation and page content live in this process; no per-page workers.
	handler := NewApp(store, token, *port)
	server := &http.Server{
		Addr: fmt.Sprintf("0.0.0.0:%d", *port), Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
	}
	listener, err := net.Listen("tcp4", server.Addr)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("pagehub %s ready on port %d; %d pages loaded", version, *port, store.Count())
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadToken(path string) (string, error) {
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
