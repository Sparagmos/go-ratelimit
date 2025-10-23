package ratelimit

import (
	"strings"
	"time"
)

type Header struct {
	Key   string
	Value string
}
type HeaderList []Header

func (h *HeaderList) String() string { return "" }
func (h *HeaderList) Set(v string) error {
	parts := strings.SplitN(v, ":", 2)
	key := strings.TrimSpace(parts[0])
	val := ""
	if len(parts) == 2 {
		val = strings.TrimSpace(parts[1])
	}
	*h = append(*h, Header{Key: key, Value: val})
	return nil
}

type Config struct {
	URL         string
	Method      string
	Bearer      string
	HTTPVersion string
	NumWorkers  int
	RunFor      time.Duration
	BodyPath    string
	Headers     HeaderList
	TestOutput  bool
}
