package ratelimit

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	TotalSent     int64
	Class2xx      int64
	Class3xx      int64
	Class4xx      int64
	Class5xx      int64
	TransportErrs int64
	LastCode      int64
	StartNanos    int64
	CodeCounts    map[int]int64
	CodeMu        sync.Mutex
}

func (m *Metrics) Start() { atomic.StoreInt64(&m.StartNanos, time.Now().UnixNano()) }

func (m *Metrics) PrintHUD() {
	el := time.Since(time.Unix(0, atomic.LoadInt64(&m.StartNanos)))
	sent := atomic.LoadInt64(&m.TotalSent)
	rps := float64(sent) / el.Seconds()
	fmt.Printf("\r\x1b[2K[%02d:%02d] sent=%d  rps=%.0f  2xx=%d  3xx=%d  4xx=%d  5xx=%d  netErr=%d  last=%d",
		int(el.Minutes()), int(el.Seconds())%60,
		sent, rps,
		atomic.LoadInt64(&m.Class2xx),
		atomic.LoadInt64(&m.Class3xx),
		atomic.LoadInt64(&m.Class4xx),
		atomic.LoadInt64(&m.Class5xx),
		atomic.LoadInt64(&m.TransportErrs),
		atomic.LoadInt64(&m.LastCode))
}
