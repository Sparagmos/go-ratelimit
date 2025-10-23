package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sparagmos/go-ratelimit/internal/ratelimit"
)

func main() {
	cfg := ratelimit.Config{}
	flag.StringVar(&cfg.URL, "url", "", "Target URL to test")
	flag.StringVar(&cfg.Method, "method", "GET", "HTTP method (GET, POST, etc.)")
	flag.StringVar(&cfg.Bearer, "bearer", "", "Bearer token (optional)")
	flag.StringVar(&cfg.HTTPVersion, "http", "1.1", "HTTP version (1.0 or 1.1)")
	flag.IntVar(&cfg.NumWorkers, "workers", 500, "Number of concurrent workers")
	flag.DurationVar(&cfg.RunFor, "duration", 60*time.Second, "How long to run the test")
	flag.StringVar(&cfg.BodyPath, "d", "", "Path to POST body file (optional)")
	flag.BoolVar(&cfg.TestOutput, "test-output", false, "Send a single request and print formatted response")
	flag.Var(&cfg.Headers, "H", "Additional header (repeatable)")

	flag.Parse()

	if cfg.URL == "" {
		fmt.Println("--url is required")
		flag.Usage()
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	if cfg.TestOutput {
		ratlimit.DoTestOutput(cfg)
		return
	}

	ratlimit.Run(ctx, cfg)
}
