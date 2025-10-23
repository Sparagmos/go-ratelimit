.PHONY: build run
build:
	go build -o bin/rate_limit ./cmd/rate_limit

run:
	go run ./cmd/rate_limit --help
