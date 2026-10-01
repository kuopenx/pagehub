// Package localhttp confines management requests and credentials to one
// numeric loopback authority. Redirects and environment proxies are disabled.
package localhttp

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func NewClient(endpoint, token string, timeout time.Duration) (*http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("management endpoint must be an HTTP loopback URL")
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
		return nil, errors.New("management endpoint must use a numeric loopback address and port")
	}
	base := http.DefaultTransport
	if standard, ok := base.(*http.Transport); ok {
		transport := standard.Clone()
		transport.Proxy = nil
		base = transport
	}
	return &http.Client{
		Transport: transport{authority: u.Host, token: token, base: base},
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("management redirects are not allowed")
		},
	}, nil
}

type transport struct {
	authority, token string
	base             http.RoundTripper
}

func (t transport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != t.authority || r.URL.User != nil ||
		(r.Host != "" && r.Host != t.authority) || (t.token != "" && r.URL.Path != "/_mcp") {
		return nil, errors.New("request is outside the configured management endpoint")
	}
	r = r.Clone(r.Context())
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(r)
}
