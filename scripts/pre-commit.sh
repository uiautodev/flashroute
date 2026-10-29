#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

unformatted="$(gofmt -l .)"
if [[ -n "${unformatted}" ]]; then
	echo "gofmt required for:"
	echo "${unformatted}"
	exit 1
fi

bash -n scripts/dev.sh scripts/pre-commit.sh scripts/build-macos-app.sh
node --check npm/bin/flashroute.js
node --check scripts/publish-npm.mjs

go vet ./...
go test ./...

if [[ -d frontend/node_modules ]]; then
	(cd frontend && npm run build)
else
	echo "skip frontend build: frontend/node_modules is missing (run npm install)"
fi

git diff --check
