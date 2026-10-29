VERSION ?= $(shell v=$$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//'); echo "$${v:-dev}")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GOTOOLCHAIN ?= auto
export GOTOOLCHAIN
BIN := bin/flashroute
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all frontend build dev run test desktop clean

all: build

frontend:
	cd frontend && npm install
	cd frontend && npm run build

build: frontend
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/flashroute

dev:
	./scripts/dev.sh

run: build
	./$(BIN)

test: frontend
	go test ./...

desktop:
	VERSION=$(VERSION) COMMIT=$(COMMIT) ./scripts/build-macos-app.sh

clean:
	rm -rf bin frontend/.svelte-kit desktop/build/bin
	rm -rf frontend/build
