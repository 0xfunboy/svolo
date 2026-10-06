#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OS="${GOOS:-$(go env GOOS)}"; ARCH="${GOARCH:-$(go env GOARCH)}"
SUFFIX=""; [[ "$OS" != windows ]] || SUFFIX=.exe
mkdir -p "$ROOT/dist/core/$OS-$ARCH"
cd "$ROOT/core"
CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -trimpath -ldflags='-s -w' -o "$ROOT/dist/core/$OS-$ARCH/svolo-core$SUFFIX" ./cmd/svolo-core
printf '%s\n' "$ROOT/dist/core/$OS-$ARCH/svolo-core$SUFFIX"
