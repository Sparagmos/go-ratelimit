# rate_limit

Refactored version of the rate-limiting tester with modular structure.

## Layout
```
rate_limit/
├── cmd/rate_limit/main.go
├── internal/ratelimit/
│   ├── config.go
│   ├── http_client.go
│   ├── metrics.go
│   ├── runner.go
│   └── test_output.go
├── go.mod
└── Makefile
```

## Build
```
make build
```

## Run
```
go run ./cmd/rate_limit --url "https://example.com/api" --method GET
```
