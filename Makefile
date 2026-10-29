VERSION ?= 1.0.0
GOTOOLCHAIN ?= auto
export GOTOOLCHAIN
BIN := bin/flashroute
LDFLAGS := -s -w -X main.version=$(VERSION)

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
	cd desktop && wails build -clean -ldflags "-X main.version=$(VERSION)"

clean:
	rm -rf bin frontend/.svelte-kit desktop/build/bin
	rm -rf frontend/build
