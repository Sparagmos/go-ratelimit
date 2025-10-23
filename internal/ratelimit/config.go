package ratelimit

import "time"

type HeaderList []string

func (h *HeaderList) String() string { return "" }
func (h *HeaderList) Set(v string) error {
	*h = append(*h, v)
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
