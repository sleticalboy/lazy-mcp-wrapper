BINARY := lazy-mcp-wrapper
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
GOCACHE ?= /tmp/lazy-mcp-wrapper-gocache
GOMODCACHE ?= /tmp/lazy-mcp-wrapper-gomodcache
GIT_TAG := $(shell git describe --tags --exact-match 2>/dev/null || echo dev)
BUILD_FLAGS := -ldflags "-X main.version=$(GIT_TAG)"
DIST_TARGETS := darwin-arm64 darwin-amd64 linux-amd64

.PHONY: build test smoke smoke-shared-daemon smoke-playwright-session install install-agent uninstall-agent dist dist-darwin-arm64 dist-darwin-amd64 dist-linux-amd64 clean

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build $(BUILD_FLAGS) -o bin/$(BINARY) ./cmd/lazy-mcp-wrapper

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

smoke: build
	./scripts/smoke.sh

smoke-shared-daemon: build
	./scripts/smoke-shared-daemon.sh

smoke-playwright-session: build
	./scripts/smoke-playwright-session.sh

install: build
	install -d $(BINDIR)
	install -m 0755 bin/$(BINARY) $(BINDIR)/$(BINARY).tmp
	mv $(BINDIR)/$(BINARY).tmp $(BINDIR)/$(BINARY)
	@echo "installed $(BINDIR)/$(BINARY)"

install-agent:
	./scripts/install-launch-agent.sh

uninstall-agent:
	./scripts/uninstall-launch-agent.sh

dist:
	rm -rf dist
	$(MAKE) $(addprefix dist-,$(DIST_TARGETS))

dist-darwin-arm64:
	@mkdir -p dist
	GOOS=darwin GOARCH=arm64 GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build $(BUILD_FLAGS) -o dist/$(BINARY)-darwin-arm64 ./cmd/lazy-mcp-wrapper
	tar -czf dist/$(BINARY)-darwin-arm64.tar.gz -C dist $(BINARY)-darwin-arm64

dist-darwin-amd64:
	@mkdir -p dist
	GOOS=darwin GOARCH=amd64 GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build $(BUILD_FLAGS) -o dist/$(BINARY)-darwin-amd64 ./cmd/lazy-mcp-wrapper
	tar -czf dist/$(BINARY)-darwin-amd64.tar.gz -C dist $(BINARY)-darwin-amd64

dist-linux-amd64:
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build $(BUILD_FLAGS) -o dist/$(BINARY)-linux-amd64 ./cmd/lazy-mcp-wrapper
	tar -czf dist/$(BINARY)-linux-amd64.tar.gz -C dist $(BINARY)-linux-amd64

clean:
	rm -rf bin dist tmp
