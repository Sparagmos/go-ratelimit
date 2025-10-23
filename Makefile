.PHONY: build run
build:
	go build -o "$(go env GOPATH)/bin/go-ratelimit" ./cmd/go_ratelimit

run:
	go run ./cmd/go_ratelimit --help
