#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-$(cd "${ROOT}" && git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')}"
VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(cd "${ROOT}" && git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
APP="${APP_OUT:-${ROOT}/desktop/build/bin/FlashRoute.app}"
BIN_DIR="$(mktemp -d)"
trap 'rm -rf "${BIN_DIR}"' EXIT

build_arch() {
	(cd "${ROOT}/desktop" && GOTOOLCHAIN=auto GOOS=darwin GOARCH="$1" CGO_ENABLED=1 \
		go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
		-o "${BIN_DIR}/flashroute-$1" .)
}

build_arch amd64
build_arch arm64

rm -rf "${APP}"
mkdir -p "${APP}/Contents/MacOS" "${APP}/Contents/Resources"
lipo -create "${BIN_DIR}/flashroute-amd64" "${BIN_DIR}/flashroute-arm64" -output "${APP}/Contents/MacOS/flashroute"
chmod +x "${APP}/Contents/MacOS/flashroute"
cp "${ROOT}/desktop/build/darwin/Info.plist" "${APP}/Contents/Info.plist"
cp "${ROOT}/desktop/build/darwin/AppIcon.icns" "${APP}/Contents/Resources/AppIcon.icns"
/usr/libexec/PlistBuddy \
	-c "Set :CFBundleVersion ${VERSION}" \
	-c "Set :CFBundleShortVersionString ${VERSION}" \
	"${APP}/Contents/Info.plist"
codesign --force --deep --sign - "${APP}"
echo "Built ${APP}"
