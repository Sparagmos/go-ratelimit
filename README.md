# go-ratelimit

A method-agnostic HTTP rate-limit / load-testing tool. Point it at an endpoint
and it fans concurrent requests at it for a set duration, showing a live HUD of
status-code counts and throughput plus a final report.

> **Authorized use only.** This tool generates load against whatever target you
> give it. Only run it against systems you own or have **written permission** to
> test. The defaults (500 workers for 60s) can overwhelm a service.

## Layout

```
go-ratelimit/
├── cmd/go_ratelimit/main.go      # CLI entrypoint (flag parsing, --raw merge)
├── internal/ratelimit/
│   ├── config.go                 # Config struct
│   ├── http_client.go            # client + request building
│   ├── metrics.go                # atomic counters + HUD line
│   ├── runner.go                 # worker pool, fail-fast, final report
│   ├── raw.go                    # Burp/curl --raw parsing
│   └── test_output.go            # single-request --test-output mode
├── go.mod
├── go.sum
└── Makefile
```

## Install

Install straight from source (no clone needed):

```sh
go install github.com/Sparagmos/go-ratelimit/cmd/go_ratelimit@latest
```

That drops a `go_ratelimit` binary in `$(go env GOPATH)/bin`. Or clone and build
with the Makefile, which stamps the version from git:

```sh
git clone https://github.com/Sparagmos/go-ratelimit.git
cd go-ratelimit
make build        # builds + installs to $(GOPATH)/bin/go-ratelimit
```

Requires **Go 1.22+**.

## Usage

```sh
# Smoke test: send ONE request and pretty-print the response (no load)
go-ratelimit --test-output --url https://authorized-target.example/api

# Load test: 200 workers for 30s, abort if 4xx pile up
go-ratelimit --url https://authorized-target.example/api \
  --workers 200 --duration 30s --max4xx 50 --fail-fast

# POST with a body and headers
go-ratelimit --url https://authorized-target.example/login \
  -X POST -H "Content-Type: application/json" -d '{"user":"x"}'

# Replay a request copied from Burp or browser devtools
go-ratelimit --raw @request.txt --duration 20s      # raw HTTP or curl, from a file
go-ratelimit --clipboard --duration 20s             # from the system clipboard
pbpaste | go-ratelimit --raw - --test-output        # from stdin
```

### Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--url`, `-u` | — | Target URL (required unless supplied via `--raw`) |
| `--method`, `-X` | `GET` | HTTP method |
| `--header`, `-H` | — | Add a header (repeatable) |
| `--bearer`, `-b` | — | Sets `Authorization: Bearer <token>` |
| `--data`, `-d` | — | Request body (use `@file` to load from disk) |
| `--http` | `1.1` | HTTP version hint |
| `--duration`, `-t` | `1m` | How long to run |
| `--workers`, `-w` | `500` | Concurrent workers |
| `--queue` | `100000` | Upper bound on queued requests |
| `--hud` | `200ms` | Live HUD refresh interval |
| `--timeout` | `10s` | Per-request timeout |
| `--fail-fast` | off | Abort on 401/403/429 or the max4xx/max5xx budgets |
| `--max4xx` / `--max5xx` | `0` (off) | Abort budget (requires `--fail-fast`) |
| `--raw` | — | Replay a pasted Burp/HTTP request or curl command (`@file`, `-` for stdin) |
| `--clipboard` | off | Read the `--raw` input from the system clipboard |
| `--scheme` | `https` | Scheme used when `--raw` gives only a path + Host |
| `--raw-index` | `0` | Pick which curl command when `--raw` holds several |
| `--insecure`, `-k` | off | Skip TLS certificate verification |
| `--test-output` | off | Send one request, print the response, exit |
| `--version`, `-v` | — | Print version and exit |

Explicit flags always win over values parsed from `--raw`, so you can replay a
captured request while overriding just the method, URL, body, or headers.
