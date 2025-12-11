package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Sparagmos/go-ratelimit/internal/ratelimit"
	flag "github.com/spf13/pflag"
)

var version = "v0.0.1-dev"

func main() {
	// --- Define flags ---
	url := flag.StringP("url", "u", "", "Target URL to test")
	method := flag.StringP("method", "X", "GET", "HTTP method (GET, POST, etc.)")
	headers := flag.StringArrayP("header", "H", []string{}, "Additional header (repeatable)")
	bearer := flag.StringP("bearer", "b", "", "Bearer token (optional)")
	dataFile := flag.StringP("data", "d", "", "Path to POST body file (optional)")
	httpVersion := flag.StringP("http", "", "1.1", "HTTP version (1.0 or 1.1)")
	duration := flag.DurationP("duration", "t", time.Minute, "How long to run the test")
	workers := flag.IntP("workers", "w", 500, "Number of concurrent workers")
	testOutput := flag.Bool("test-output", false, "Send a single request and print formatted response")
	versionFlag := flag.BoolP("version", "v", false, "Print version and exit")

	flag.Parse()

	// --- Check for version flag
	if *versionFlag {
		fmt.Println("go-ratelimit", version)
		os.Exit(0)
	}

	if *url == "" {
		fmt.Println("Error: --url (-u) is required")
		os.Exit(1)
	}

	// --- Prepare headers ---
	var hdr ratelimit.HeaderList
	for _, h := range *headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			hdr = append(hdr, ratelimit.Header{
				Key:   strings.TrimSpace(parts[0]),
				Value: strings.TrimSpace(parts[1]),
			})
		}
	}
	if *bearer != "" {
		hdr = append(hdr, ratelimit.Header{
			Key:   "Authorization",
			Value: "Bearer " + *bearer,
		})
	}

	// --- Build config ---
	cfg := ratelimit.Config{
		URL:         *url,
		Method:      *method,
		Headers:     hdr,
		Bearer:      *bearer,
		BodyPath:    *dataFile,
		HTTPVersion: *httpVersion,
		RunFor:      *duration,
		NumWorkers:  *workers,
	}

	// --- Handle test-output mode ---
	if *testOutput {
		ratelimit.DoTestOutput(cfg)
		return
	}

	// --- Normal run ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// listen for Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Println("\nAborting...")
		cancel()
	}()

	if err := ratelimit.Run(ctx, cfg); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
