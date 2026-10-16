package ratelimit

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	code int
	err  error
	dur  time.Duration
}

// Run drives a full load test: it queues up to cfg.Queue requests, fans them
// out across cfg.NumWorkers workers, collects per-code metrics, renders a live
// HUD, and prints a final report. It returns when the run finishes — because
// ctx is cancelled (timeout, Ctrl+C, or a fail-fast abort) or the queue drains.
func Run(ctx context.Context, cfg Config) error {
	m := &Metrics{CodeCounts: make(map[int]int64)}
	m.Start()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	reqs := make(chan struct{}, 8192)
	results := make(chan result, 8192)

	// Producer: queue work quickly, bounded by cfg.Queue.
	go func() {
		defer close(reqs)
		for i := int64(0); i < cfg.Queue; i++ {
			select {
			case <-ctx.Done():
				return
			case reqs <- struct{}{}:
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < cfg.NumWorkers; i++ {
		wg.Add(1)
		go worker(ctx, &wg, cfg, reqs, results)
	}

	// Collector: single goroutine owns the counters and the fail-fast decision.
	var collectorWG sync.WaitGroup
	collectorWG.Add(1)
	go func() {
		defer collectorWG.Done()
		collect(cfg, m, results, cancel)
	}()

	// Live HUD.
	hudEvery := cfg.HUDEvery
	if hudEvery <= 0 {
		hudEvery = 200 * time.Millisecond
	}
	doneHUD := make(chan struct{})
	go liveHUD(ctx, m, doneHUD, hudEvery)

	wg.Wait()
	close(results)
	collectorWG.Wait()

	close(doneHUD)
	time.Sleep(50 * time.Millisecond) // let the HUD clear its line once

	finalReport(m)
	return nil
}

func worker(ctx context.Context, wg *sync.WaitGroup, cfg Config, reqs <-chan struct{}, results chan<- result) {
	defer wg.Done()
	client := newClient(cfg.Timeout, cfg.Insecure)

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-reqs:
			if !ok {
				return
			}
			req, err := buildRequest(cfg)
			if err != nil {
				results <- result{err: err}
				continue
			}
			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				results <- result{err: err}
				continue
			}
			code := resp.StatusCode
			_ = resp.Body.Close()
			results <- result{code: code, dur: time.Since(start)}
		}
	}
}

func collect(cfg Config, m *Metrics, results <-chan result, cancel context.CancelFunc) {
	for r := range results {
		if r.err != nil {
			atomic.AddInt64(&m.TransportErrs, 1)
			atomic.StoreInt64(&m.LastCode, -1)
			continue
		}
		atomic.AddInt64(&m.TotalSent, 1)
		atomic.StoreInt64(&m.LastCode, int64(r.code))

		switch r.code / 100 {
		case 2:
			atomic.AddInt64(&m.Class2xx, 1)
		case 3:
			atomic.AddInt64(&m.Class3xx, 1)
		case 4:
			atomic.AddInt64(&m.Class4xx, 1)
		case 5:
			atomic.AddInt64(&m.Class5xx, 1)
		}

		m.CodeMu.Lock()
		m.CodeCounts[r.code]++
		m.CodeMu.Unlock()

		// Fail-fast: abort the run on a hard block (401/403/429) or once the
		// configured 4xx/5xx budgets are exhausted.
		if cfg.FailFast &&
			(r.code == 401 || r.code == 403 || r.code == 429 ||
				(cfg.Max4xx > 0 && atomic.LoadInt64(&m.Class4xx) >= cfg.Max4xx) ||
				(cfg.Max5xx > 0 && atomic.LoadInt64(&m.Class5xx) >= cfg.Max5xx)) {
			cancel()
		}
	}
}

func liveHUD(ctx context.Context, m *Metrics, done <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-done:
			clearLine()
			return
		case <-ctx.Done():
			clearLine()
			return
		case <-t.C:
			m.PrintHUD()
		}
	}
}

func clearLine() { fmt.Print("\r\x1b[2K") }

func finalReport(m *Metrics) {
	el := time.Since(time.Unix(0, atomic.LoadInt64(&m.StartNanos)))
	sent := atomic.LoadInt64(&m.TotalSent)

	clearLine()
	fmt.Println("---- Final Report ----")
	fmt.Printf("Elapsed: %02d:%02d\n", int(el.Minutes()), int(el.Seconds())%60)
	fmt.Printf("Total sent: %d (avg RPS: %.0f)\n", sent, float64(sent)/el.Seconds())
	fmt.Printf("2xx: %d | 3xx: %d | 4xx: %d | 5xx: %d | transport errors: %d\n",
		atomic.LoadInt64(&m.Class2xx),
		atomic.LoadInt64(&m.Class3xx),
		atomic.LoadInt64(&m.Class4xx),
		atomic.LoadInt64(&m.Class5xx),
		atomic.LoadInt64(&m.TransportErrs))

	m.CodeMu.Lock()
	type kv struct {
		code  int
		count int64
	}
	var arr []kv
	for c, k := range m.CodeCounts {
		arr = append(arr, kv{c, k})
	}
	m.CodeMu.Unlock()

	sort.Slice(arr, func(i, j int) bool { return arr[i].count > arr[j].count })
	if len(arr) > 0 {
		fmt.Println("Status code breakdown:")
		maxShow := len(arr)
		if maxShow > 15 {
			maxShow = 15
		}
		for i := 0; i < maxShow; i++ {
			fmt.Printf("  %d: %d\n", arr[i].code, arr[i].count)
		}
	}
}
