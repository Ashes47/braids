GO      ?= go
BIN     := braids
PKGS    := ./...

.PHONY: all build test race lint vet fmt tidy cover clean ci

all: build

build:
	$(GO) build -trimpath -o $(BIN) ./cmd/braids

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
