package ratelimit

import (
	"context"
	"fmt"
	"net/http"
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

func Run(ctx context.Context, cfg Config) error {
	m := &Metrics{CodeCounts: make(map[int]int64)}
	m.Start()

	client := newClient()
	var wg sync.WaitGroup

	reqs := make(chan struct{}, 8192)
	results := make(chan result, 8192)

	go func() {
		for i := 0; i < cfg.NumWorkers*100; i++ {
			select {
			case <-ctx.Done():
				close(reqs)
				return
			case reqs <- struct{}{}:
			}
		}
		close(reqs)
	}()

	for i := 0; i < cfg.NumWorkers; i++ {
		wg.Add(1)
		go worker(ctx, &wg, client, cfg, reqs, results)
	}

	go collector(ctx, m, results)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			close(results)
			finalReport(m)
			return nil
		case <-ticker.C:
			m.PrintHUD()
		}
	}
}

func worker(ctx context.Context, wg *sync.WaitGroup, client *http.Client, cfg Config, reqs <-chan struct{}, results chan<- result) {
	defer wg.Done()
	for range reqs {
		select {
		case <-ctx.Done():
			return
		default:
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

func collector(ctx context.Context, m *Metrics, results <-chan result) {
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
	}
}

func finalReport(m *Metrics) {
	fmt.Println("\n---- Final Report ----")
	el := time.Since(time.Unix(0, atomic.LoadInt64(&m.StartNanos)))
	sent := atomic.LoadInt64(&m.TotalSent)
	fmt.Printf("Elapsed: %02d:%02d\n", int(el.Minutes()), int(el.Seconds())%60)
	fmt.Printf("Total sent: %d (avg RPS: %.0f)\n", sent, float64(sent)/el.Seconds())
	fmt.Printf("2xx: %d | 3xx: %d | 4xx: %d | 5xx: %d | transport errors: %d\n",
		atomic.LoadInt64(&m.Class2xx),
		atomic.LoadInt64(&m.Class3xx),
		atomic.LoadInt64(&m.Class4xx),
		atomic.LoadInt64(&m.Class5xx),
		atomic.LoadInt64(&m.TransportErrs))

	m.CodeMu.Lock()
	var arr []struct {
		code  int
		count int64
	}
	for c, k := range m.CodeCounts {
		arr = append(arr, struct {
			code  int
			count int64
		}{c, k})
	}
	m.CodeMu.Unlock()
	sort.Slice(arr, func(i, j int) bool { return arr[i].count > arr[j].count })
	if len(arr) > 0 {
		fmt.Println("Status code breakdown:")
		for i, kv := range arr {
			if i >= 15 {
				break
			}
			fmt.Printf("  %d: %d\n", kv.code, kv.count)
		}
	}
}
