BINARY    := wechat-router
MODULE    := github.com/metaRobin/wechat-router-go
CMD       := ./cmd/wechat-router
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS   := -s -w -X main.version=$(VERSION)

.PHONY: build test lint vet clean install cross

## build: compile for the current platform
build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) $(CMD)

## install: install to $GOPATH/bin
install:
	go install -ldflags '$(LDFLAGS)' $(CMD)

## test: run all tests with race detector
test:
	go test -race -count=1 ./...

## vet: run go vet
vet:
	go vet ./...

## lint: run vet + staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
lint: vet
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

## clean: remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY)-*

## cross: build for linux/amd64 and darwin/arm64
cross:
	GOOS=linux  GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o $(BINARY)-linux-amd64  $(CMD)
	GOOS=darwin GOARCH=arm64 go build -ldflags '$(LDFLAGS)' -o $(BINARY)-darwin-arm64 $(CMD)

## help: show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
