package tools

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/dark-agents/research-mcp/internal/config"
)

// httpClient is a configured *http.Client with a Tor-aware dialer.
//
// When tor is true, all requests go through the configured SOCKS5 proxy
// (cfg.Tor.SOCKS5URL). Otherwise, the system resolver and default dialer
// are used.
type httpClient struct {
	cfg config.Config
	hc  *http.Client
}

func newHTTPClient(cfg config.Config, tor bool) *httpClient {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if tor && cfg.Tor.SOCKS5URL != "" {
		// SOCKS5 proxy URL parsing: golang.org/x/net/proxy is the standard
		// client. We import it lazily here to keep this file's deps narrow.
		// Setting tr.Proxy = http.ProxyURL(parsed) routes all requests.
		if u, err := url.Parse(cfg.Tor.SOCKS5URL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}

	return &httpClient{
		cfg: cfg,
		hc: &http.Client{
			Timeout:   cfg.RequestTimeout(),
			Transport: tr,
		},
	}
}

// Do sends req and returns the response. Body is the caller's responsibility.
func (c *httpClient) Do(req *http.Request) (*http.Response, error) {
	return c.hc.Do(req)
}

// DoContext is like Do but accepts a context for cancellation/deadlines.
func (c *httpClient) DoContext(ctx context.Context, req *http.Request) (*http.Response, error) {
	return c.hc.Do(req.WithContext(ctx))
}