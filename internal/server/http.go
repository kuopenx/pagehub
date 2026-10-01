package server

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed dashboard.html
var dashboardHTML string
var dashboardTemplate = template.Must(template.New("dashboard").Parse(dashboardHTML))

type App struct {
	store *Store
	mcp   http.Handler
	port  int
}

func NewApp(store *Store, token string, port int) *App {
	server := newMCP(store, port)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: -1,
	})
	return &App{store: store, mcp: managementOnly(token, port, handler), port: port}
}

func managementOnly(token string, port int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "MCP management is local-only", http.StatusForbidden)
			return
		}
		if !localAuthority(r.Host, port) {
			http.Error(w, "invalid management host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || !localAuthority(u.Host, port) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
				http.Error(w, "invalid origin", http.StatusForbidden)
				return
			}
		}
		expected := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "management token required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localAuthority(authority string, port int) bool {
	host, p, err := net.SplitHostPort(authority)
	if err != nil || p != strconv.Itoa(port) {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if r.URL.Path == "/_mcp" {
		a.mcp.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	switch r.URL.Path {
	case "/":
		pages := a.store.List(r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		data := struct {
			Query   string
			Pages   []Page
			Count   int
			LANURLs []string
			Version string
		}{r.URL.Query().Get("q"), pages, a.store.Count(), lanURLs(a.port, "/"), version}
		if err := dashboardTemplate.Execute(w, data); err != nil {
			return
		}
		return
	case "/_health":
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": version, "pages": a.store.Count(), "protocol": "2026-07-28"})
		}
		return
	case "/favicon.ico":
		w.WriteHeader(http.StatusNoContent)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	id := strings.TrimSuffix(path, "/")
	if !idPattern.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	if path == id {
		if a.storeHas(id) {
			http.Redirect(w, r, "/"+id+"/", http.StatusTemporaryRedirect)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	f, p, err := a.store.Open(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%d"`, p.ID, p.UpdatedAt.UnixNano()))
	http.ServeContent(w, r, "index.html", p.UpdatedAt, f)
}

func (a *App) storeHas(id string) bool {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	_, ok := a.store.pages[id]
	return ok
}
