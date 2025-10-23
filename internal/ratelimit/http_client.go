package ratelimit

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
)

// hasHeader checks if cfg.Headers already contains a header with the given key (case-insensitive).
func hasHeader(headers HeaderList, key string) bool {
	keyLower := strings.ToLower(strings.TrimSpace(key))
	for _, h := range headers {
		if strings.ToLower(strings.TrimSpace(h.Key)) == keyLower {
			return true
		}
	}
	return false
}

func buildRequest(cfg Config) (*http.Request, error) {
	var body io.Reader
	if cfg.BodyPath != "" {
		data, err := os.ReadFile(cfg.BodyPath)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(cfg.Method, cfg.URL, body)
	if err != nil {
		return nil, err
	}

	// Apply structured headers
	for _, h := range cfg.Headers {
		key := strings.TrimSpace(h.Key)
		val := strings.TrimSpace(h.Value)
		req.Header.Set(key, val)
	}

	// Add Bearer token if missing
	if strings.TrimSpace(cfg.Bearer) != "" && !hasHeader(cfg.Headers, "Authorization") {
		b := strings.TrimSpace(cfg.Bearer)
		if !strings.HasPrefix(strings.ToLower(b), "bearer ") {
			b = "Bearer " + b
		}
		req.Header.Set("Authorization", b)
	}

	// Set HTTP version string for visibility
	req.Proto = "HTTP/" + cfg.HTTPVersion

	return req, nil
}

func newClient() *http.Client {
	return &http.Client{}
}
