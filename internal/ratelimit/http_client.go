package ratelimit

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// onceWarnNoBody ensures the "method does not use a body" note prints at most
// once per process, even across hundreds of concurrent workers.
var onceWarnNoBody sync.Once

// newClient builds an HTTP client, optionally skipping TLS verification (curl -k).
func newClient(timeout time.Duration, insecure bool) *http.Client {
	if !insecure {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// buildRequest constructs a fresh *http.Request from cfg. A new body reader is
// created on every call so the request is safe to send repeatedly from workers.
func buildRequest(cfg Config) (*http.Request, error) {
	var body io.Reader
	if len(cfg.Body) > 0 {
		if methodAllowsBody(cfg.Method) {
			body = bytes.NewReader(cfg.Body)
		} else {
			onceWarnNoBody.Do(func() {
				fmt.Fprintln(os.Stderr, "note: method does not use a body; ignoring --data")
			})
		}
	}

	req, err := http.NewRequest(cfg.Method, cfg.URL, body)
	if err != nil {
		return nil, err
	}

	for k, vv := range cfg.Headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	// Add Authorization: Bearer unless an Authorization header is already set.
	if strings.TrimSpace(cfg.Bearer) != "" && req.Header.Get("Authorization") == "" {
		b := strings.TrimSpace(cfg.Bearer)
		if !strings.HasPrefix(strings.ToLower(b), "bearer ") {
			b = "Bearer " + b
		}
		req.Header.Set("Authorization", b)
	}

	if cfg.HostOverride != "" {
		req.Host = cfg.HostOverride
	}

	// Protocol hint; the Transport ultimately decides the wire protocol.
	req.Proto = "HTTP/" + strings.TrimSpace(cfg.HTTPVersion)

	return req, nil
}

// methodAllowsBody reports whether a request body is meaningful for the method.
func methodAllowsBody(m string) bool {
	switch strings.ToUpper(m) {
	case "GET", "HEAD", "TRACE":
		return false
	default:
		return true
	}
}

// classifyRequestError turns a raw net/http error into a short, human
// explanation so single-request failures read like the rest of the tool's
// output instead of a bare Go error dump.
func classifyRequestError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused — nothing is listening on that host/port, or it's actively rejecting the connection"
	case strings.Contains(msg, "no such host"):
		return "DNS lookup failed — the hostname could not be resolved"
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "i/o timeout"):
		return "timed out — no response within the configured --timeout"
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"):
		return "TLS certificate error — pass --insecure (-k) to skip verification if this is an internal/self-signed cert"
	case strings.Contains(msg, "connection reset"):
		return "connection reset by peer"
	case strings.Contains(msg, "network is unreachable"):
		return "network unreachable — no route to host"
	default:
		return msg
	}
}
