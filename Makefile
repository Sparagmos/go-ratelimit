# --- go-ratelimit Makefile ---

APP_NAME := go-ratelimit
PKG_PATH := ./cmd/go_ratelimit
BIN_PATH := $(GOPATH)/bin/$(APP_NAME)

# dynamically pull version info from git
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags="-X 'main.version=$(VERSION)'"

# default build
build:
	@echo "🔨 Building $(APP_NAME) $(VERSION)..."
	go build $(LDFLAGS) -o $(BIN_PATH) $(PKG_PATH)
	@echo "✅ Installed to $(BIN_PATH)"

# install just symlinks or reuses build output (handy if bin already in PATH)
install: build
	@echo "✨ Installed $(APP_NAME) $(VERSION)"

# quick test run
run:
	go run $(LDFLAGS) $(PKG_PATH)

# clean out binary
clean:
	@rm -f $(BIN_PATH)
	@echo "🧹 Cleaned up $(BIN_PATH)"