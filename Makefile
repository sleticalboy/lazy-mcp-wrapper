BINARY := lazy-mcp-wrapper
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
GOCACHE ?= /private/tmp/lazy-mcp-wrapper-gocache

.PHONY: build test smoke install install-agent uninstall-agent clean

build:
	GOCACHE=$(GOCACHE) go build -o bin/$(BINARY) ./cmd/lazy-mcp-wrapper

test:
	GOCACHE=$(GOCACHE) go test ./...

smoke: build
	./scripts/smoke.sh

install: build
	install -d $(BINDIR)
	install -m 0755 bin/$(BINARY) $(BINDIR)/$(BINARY).tmp
	mv $(BINDIR)/$(BINARY).tmp $(BINDIR)/$(BINARY)
	@echo "installed $(BINDIR)/$(BINARY)"

install-agent:
	./scripts/install-launch-agent.sh

uninstall-agent:
	./scripts/uninstall-launch-agent.sh

clean:
	rm -rf bin dist tmp
