package server

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed dashboard.html
var dashboardHTML string
var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{"hue": pageHue}).Parse(dashboardHTML))

// pageHue derives a stable hue for a card's folded corner so pages stay visually distinct.
func pageHue(id string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(id))
	return h.Sum32() % 360
}

type App struct {
	store *Store
	mcp   http.Handler
	port  int
}

func NewApp(store *Store, token string, port int) *App {
	return newApp(store, func(candidate string) bool {
		return token != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
	}, port)
}

func newApp(store *Store, authorize func(string) bool, port int) *App {
	server := newMCP(store, port)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: -1,
		// Bearer tokens are the access boundary, including LAN clients.
		DisableLocalhostProtection: true,
	})
	return &App{store: store, mcp: bearerOnly(authorize, handler), port: port}
}

func bearerOnly(authorize func(string) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check current credentials on every request for immediate revocation.
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" || !authorize(token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "management token required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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
			_ = json.NewEncoder(w).Encode(map[string]any{"service": "pagehub", "pid": os.Getpid(), "status": "ok", "version": version, "pages": a.store.Count(), "protocol": "2026-07-28"})
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
