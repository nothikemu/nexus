# Nexus — developer tasks. Run `make help` for a list.

BINARY   := nexus
PKG      := github.com/nothikemu/nexus
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@2025.1.1

.PHONY: help build install test test-race integration lint fmt vet staticcheck clean

help: ## show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## build ./bin/nexus
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/nexus

install: ## install nexus into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/nexus

test: ## unit tests (integration tests skip without NEXUS_TEST_DATABASE_URL)
	go test ./...

test-race: ## all tests with the race detector
	go test -race -count=1 ./...

integration: ## integration tests against $$NEXUS_TEST_DATABASE_URL
	@test -n "$$NEXUS_TEST_DATABASE_URL" || (echo "set NEXUS_TEST_DATABASE_URL, e.g. postgres://postgres:postgres@localhost:5432/postgres" && exit 1)
	go test -race -count=1 ./...

lint: fmt-check vet staticcheck ## gofmt, go vet and staticcheck

fmt: ## format the code
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

staticcheck:
	go run $(STATICCHECK) ./...

clean: ## remove build output
	rm -rf bin dist
