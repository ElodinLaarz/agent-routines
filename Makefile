SHELL := /bin/bash
BIN := routines
PKG := github.com/ElodinLaarz/agent-routines

VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build test lint vet run clean fmt

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/routines

test:
	go test ./...

lint:
	golangci-lint run

vet:
	go vet ./...

fmt:
	gofmt -w -s .

run: build
	./bin/$(BIN) daemon

clean:
	rm -rf bin
