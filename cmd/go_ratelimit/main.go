package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Sparagmos/go-ratelimit/internal/ratelimit"
	flag "github.com/spf13/pflag"
)

var version = "v0.0.1-dev"

func main() {
	// --- Request shape ---
	url := flag.StringP("url", "u", "", "Target URL to test")
	method := flag.StringP("method", "X", "GET", "HTTP method (GET, POST, PUT, PATCH, DELETE, ...)")
	headers := flag.StringArrayP("header", "H", nil, `HTTP header, e.g. -H "Content-Type: application/json" (repeatable)`)
	bearer := flag.StringP("bearer", "b", "", "Bearer token (sets Authorization unless overridden by -H)")
	data := flag.StringP("data", "d", "", "Request body data. Use @path/to/file to load from a file.")
	httpVersion := flag.String("http", "1.1", "HTTP version hint: 1.0 or 1.1 (Transport decides; this is a hint)")

	// --- Load profile ---
	duration := flag.DurationP("duration", "t", time.Minute, "How long to run (e.g. 30s, 2m)")
	workers := flag.IntP("workers", "w", 500, "Number of concurrent workers")
	queue := flag.Int64("queue", 100000, "Max requests to queue (upper bound)")
	hud := flag.Duration("hud", 200*time.Millisecond, "Live HUD update interval")
	timeout := flag.Duration("timeout", 10*time.Second, "Per-request client timeout")

	// --- Fail-fast / aborts ---
	failFast := flag.Bool("fail-fast", false, "Cancel the run when a fail-fast condition hits (401/403/429 or the max4xx/max5xx budgets)")
	max4xx := flag.Int64("max4xx", 0, "Abort (with --fail-fast) when total 4xx >= this (0 = disabled)")
	max5xx := flag.Int64("max5xx", 0, "Abort (with --fail-fast) when total 5xx >= this (0 = disabled)")

	// --- Raw request replay ---
	raw := flag.String("raw", "", "Paste a raw Burp/HTTP request or curl command (auto-detected). Use @path to load a file, or - for stdin. -X/-H/-d/--url still override parsed values.")
	clipboard := flag.Bool("clipboard", false, "Read the raw request/curl command from the system clipboard instead of --raw (macOS: pbpaste, Linux: xclip/xsel/wl-paste, Windows: Get-Clipboard)")
	scheme := flag.String("scheme", "https", "Scheme used to build the URL when --raw supplies only a path + Host header")
	rawIndex := flag.Int("raw-index", 0, "When --raw/--clipboard holds multiple curl commands, pick which one to use (0-based)")

	// --- TLS / modes ---
	insecure := flag.BoolP("insecure", "k", false, "Skip TLS certificate verification (like curl -k)")
	testOutput := flag.Bool("test-output", false, "Send a single request, pretty-print the response, then exit")
	versionFlag := flag.BoolP("version", "v", false, "Print version and exit")

	flag.Parse()

	if *versionFlag {
		fmt.Println("go-ratelimit", version)
		os.Exit(0)
	}

	// Normalize method.
	m := strings.ToUpper(strings.TrimSpace(*method))
	if m == "" {
		m = "GET"
	}

	targetURL := *url
	hdr := ratelimit.BuildHeaderFromList(*headers)
	useInsecure := *insecure
	var hostOverride string

	// Load the request body (inline or @file).
	var bodyBytes []byte
	if *data != "" {
		b, err := ratelimit.LoadMaybeFile(*data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fatal: failed to read body data: %v\n", err)
			os.Exit(1)
		}
		bodyBytes = b
	}

	// --- Merge in a pasted Burp/raw-HTTP request or curl command ---
	if *raw != "" || *clipboard {
		content, source, err := ratelimit.LoadRawSource(*raw, *clipboard)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fatal: failed to read --raw input: %v\n", err)
			os.Exit(1)
		}
		parsed, format, candidates, err := ratelimit.ParseRaw(content, *rawIndex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fatal: failed to parse --raw input (source: %s): %v\n", source, err)
			os.Exit(1)
		}

		if len(candidates) > 1 {
			fmt.Fprintf(os.Stderr, "[raw:%s] detected %d curl commands; using #%d (pass --raw-index N to pick another):\n", source, len(candidates), *rawIndex)
			maxShow := len(candidates)
			if maxShow > 20 {
				maxShow = 20
			}
			for i := 0; i < maxShow; i++ {
				marker := " "
				if i == *rawIndex {
					marker = "*"
				}
				u := candidates[i].URL
				if len(u) > 100 {
					u = u[:97] + "..."
				}
				fmt.Fprintf(os.Stderr, "  %s #%-3d %-8s %s\n", marker, i, candidates[i].Method, u)
			}
			if len(candidates) > maxShow {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(candidates)-maxShow)
			}
		}

		// Parsed values fill gaps only; explicit flags win.
		if !flag.CommandLine.Changed("method") && parsed.Method != "" {
			m = parsed.Method
		}
		if !flag.CommandLine.Changed("url") {
			switch {
			case strings.HasPrefix(parsed.URL, "http://") || strings.HasPrefix(parsed.URL, "https://"):
				targetURL = parsed.URL
			case parsed.URL != "" && parsed.Host != "":
				targetURL = strings.TrimSuffix(*scheme, "://") + "://" + parsed.Host + parsed.URL
			case parsed.URL != "":
				fmt.Fprintln(os.Stderr, "fatal: --raw request has no Host header and no absolute URL; pass --url explicitly or include a Host header")
				os.Exit(1)
			}
		}
		hostOverride = parsed.Host

		hdr = ratelimit.MergeHeaders(parsed.Headers, hdr)

		if !flag.CommandLine.Changed("data") && len(parsed.Body) > 0 {
			bodyBytes = parsed.Body
		}
		if parsed.Insecure && !flag.CommandLine.Changed("insecure") {
			useInsecure = true
		}

		fmt.Fprintf(os.Stderr, "[raw:%s] parsed %s request: method=%s url=%s headers=%d bodyBytes=%d", source, format, m, targetURL, len(hdr), len(bodyBytes))
		if hostOverride != "" {
			fmt.Fprintf(os.Stderr, " host=%s", hostOverride)
		}
		if useInsecure {
			fmt.Fprint(os.Stderr, " insecure=true")
		}
		fmt.Fprintln(os.Stderr)
	}

	// Bearer token (added in the client builder unless Authorization is already set).
	if targetURL == "" {
		fmt.Fprintln(os.Stderr, "Error: --url (-u) is required (or supply one via --raw/--clipboard)")
		os.Exit(1)
	}

	cfg := ratelimit.Config{
		URL:          targetURL,
		Method:       m,
		Headers:      hdr,
		Bearer:       *bearer,
		Body:         bodyBytes,
		HTTPVersion:  *httpVersion,
		RunFor:       *duration,
		NumWorkers:   *workers,
		Queue:        *queue,
		HUDEvery:     *hud,
		Timeout:      *timeout,
		FailFast:     *failFast,
		Max4xx:       *max4xx,
		Max5xx:       *max5xx,
		HostOverride: hostOverride,
		Insecure:     useInsecure,
		TestOutput:   *testOutput,
	}

	// --- test-output mode: a single request, no load ---
	if cfg.TestOutput {
		ratelimit.DoTestOutput(cfg)
		return
	}

	// --- Full run ---
	ctx, cancel := context.WithTimeout(context.Background(), cfg.RunFor)
	defer cancel()
	cancelOnSignal(cancel)

	if err := ratelimit.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cancelOnSignal(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		fmt.Fprintln(os.Stderr, "\nAborting...")
		cancel()
	}()
}
