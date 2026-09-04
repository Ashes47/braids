GO      ?= go
BIN     := braids
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)
PKGS    := ./...

.PHONY: all build install run reindex test race lint vet fmt tidy cover clean ci \
	site site-build pages frames

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

# lint runs the same version CI does. Pinned rather than "latest" so a new
# release cannot turn the build red without a commit saying so, and run through
# `go run` so there is nothing to install first — a lint gate you have to set up
# by hand is one that only ever runs on CI, which is where these rules were
# being discovered.
LINTER := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2

lint:
	$(GO) run $(LINTER) run

tidy:
	$(GO) mod tidy && git diff --exit-code go.mod go.sum

# site serves braids.chat locally at http://localhost:8787. The page is one
# static file with no build step, so this is a plain file server — the same
# thing GitHub Pages does.
# site serves the generated pages on localhost. The pages are not committed;
# the generator, the captured frames and assets/ are.
site: pages
	@echo "braids.chat -> http://localhost:8787  (ctrl-c to stop)"
	@python3 -m http.server 8787 --directory site

# pages rebuilds the landing page and the docs from frames already captured.
pages:
	python3 site/build.py
	python3 site/docs.py

# frames recaptures the screenshots, which needs the binary. They are real
# braids output taken against a fake ~/.claude, never drawn by hand. 195
# columns is the width where the header draws the facts, the glyph key, every
# binding and the full mark.
frames: install
	python3 scripts/demo.py --out /tmp/braids-demo --frames site/frames \
		--width 195 --braids "$(shell go env GOPATH)/bin/braids" >/dev/null
	for f in map spine search; do \
		python3 scripts/ansi2svg.py site/frames/$$f.ans assets/frames/$$f.svg; \
	done

# site-build does the lot: recapture, then regenerate.
site-build: frames pages

clean:
	rm -f $(BIN) coverage.out

ci: fmt vet lint test race cover
