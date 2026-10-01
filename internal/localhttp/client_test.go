package localhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRedirectsNeverForwardCredentials(t *testing.T) {
	var remoteCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { remoteCalls.Add(1) }))
	defer remote.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("local request missing credentials")
				}
				http.Redirect(w, r, remote.URL+"/_mcp", code)
			}))
			defer local.Close()
			client, err := NewClient(local.URL+"/_mcp", "test-token", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			if _, err = client.Get(local.URL + "/_mcp"); err == nil {
				t.Fatal("redirect accepted")
			}
		})
	}
	if remoteCalls.Load() != 0 {
		t.Fatal("redirect target was contacted")
	}
}

func TestTransportRejectsUnpinnedRequestsBeforeSending(t *testing.T) {
	called := 0
	transport := transport{authority: "127.0.0.1:8765", token: "test-token", base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	for _, endpoint := range []string{"http://remote.invalid:8765/_mcp", "http://127.0.0.1:8766/_mcp", "https://127.0.0.1:8765/_mcp", "http://127.0.0.1:8765/other", "http://user@127.0.0.1:8765/_mcp"} {
		req, _ := http.NewRequest("GET", endpoint, nil)
		if _, err := transport.RoundTrip(req); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:8765/_mcp", nil)
	req.Host = "remote.invalid"
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("accepted custom Host")
	}
	if called != 0 {
		t.Fatal("unsafe request reached transport")
	}
	req.Host = ""
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if called != 1 || req.Header.Get("Authorization") != "" {
		t.Fatal("valid request or cloning failed")
	}
}

func TestEndpointValidationAndProxyDisabled(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:8765/_mcp", "http://remote.invalid/_mcp", "http://127.0.0.1/_mcp", "http://127.0.0.1:0/_mcp", "http://127.0.0.1:65536/_mcp", "http://127.0.0.1:8765/_mcp?q=1", "http://127.0.0.1:8765/_mcp#frag", "http://127.0.0.1:8765@remote.invalid/_mcp"} {
		if _, err := NewClient(endpoint, "token", time.Second); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:8765/_mcp", "http://[::1]:8765/_mcp"} {
		client, err := NewClient(endpoint, "token", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if client.Transport.(transport).base.(*http.Transport).Proxy != nil {
			t.Fatal("environment proxy enabled")
		}
		client.CloseIdleConnections()
	}
}
