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

go vet ./...
go test ./...

if [[ -d frontend/node_modules ]]; then
	(cd frontend && npm run build)
else
	echo "skip frontend build: frontend/node_modules is missing (run npm install)"
fi

git diff --check
