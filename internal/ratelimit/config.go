package ratelimit

import (
	"net/http"
	"time"
)

// Config holds everything a run (or a single --test-output request) needs.
// The cmd layer is responsible for populating it from flags and from any
// parsed --raw / curl input; this package just consumes it.
type Config struct {
	URL         string      // absolute target URL
	Method      string      // HTTP method (already upper-cased by the caller)
	Headers     http.Header // request headers to send on every request
	Bearer      string      // bearer token; adds Authorization unless one is already set
	Body        []byte      // request body (used only for methods that allow one)
	HTTPVersion string      // "1.0" / "1.1" protocol hint

	RunFor     time.Duration // how long a full run lasts
	NumWorkers int           // concurrent workers
	Queue      int64         // upper bound on queued requests
	HUDEvery   time.Duration // live HUD refresh interval
	Timeout    time.Duration // per-request client timeout

	FailFast bool  // cancel the run early when an abort condition is hit
	Max4xx   int64 // abort (with FailFast) once total 4xx >= this (0 = disabled)
	Max5xx   int64 // abort (with FailFast) once total 5xx >= this (0 = disabled)

	HostOverride string // sets req.Host (SNI/Host header) independent of the URL host
	Insecure     bool   // skip TLS certificate verification (curl -k)

	TestOutput bool // send a single request, pretty-print it, and exit
}
