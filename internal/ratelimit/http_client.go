package ratelimit

import (
	"bytes"
	"io"
	"net/http"
	"os"
)

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
	for _, hdr := range cfg.Headers {
		parts := bytes.SplitN([]byte(hdr), []byte(":"), 2)
		if len(parts) == 2 {
			req.Header.Set(string(bytes.TrimSpace(parts[0])), string(bytes.TrimSpace(parts[1])))
		}
	}
	if cfg.Bearer != "" {
		req.Header.Set("Authorization", cfg.Bearer)
	}
	req.Proto = "HTTP/" + cfg.HTTPVersion
	return req, nil
}

func newClient() *http.Client {
	return &http.Client{}
}
