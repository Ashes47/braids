GO      ?= go
BIN     := braids
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)
PKGS    := ./...

.PHONY: all build install run reindex test race lint vet fmt tidy cover clean ci

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/braids

# install puts braids on your PATH (needs $(shell $(GO) env GOPATH)/bin in PATH).
install:
	$(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/braids

# run builds and opens the map without installing.
run: build
	./$(BIN)

# reindex rebuilds the search index from ~/.claude/projects.
reindex: build
	./$(BIN) index

test:
	$(GO) test $(PKGS)

race:
	$(GO) test -race $(PKGS)

cover:
	$(GO) test -coverprofile=coverage.out -covermode=atomic $(PKGS)
	$(GO) tool cover -func=coverage.out | tail -1

vet:
	$(GO) vet $(PKGS)

fmt:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "run gofmt -w ." && exit 1)

lint:
	golangci-lint run

tidy:
	$(GO) mod tidy && git diff --exit-code go.mod go.sum

clean:
	rm -f $(BIN) coverage.out

ci: fmt vet test race cover
