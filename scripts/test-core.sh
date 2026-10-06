#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../core"
go vet ./...
go test -race -count=1 ./...
if [[ "${SVOLO_E2E:-0}" == 1 ]]; then
  # Run as an ordinary user with the browser sandbox enabled.
  # Root in an isolated CI container additionally needs explicit test-only opt-in.
  go test -race -count=1 ./internal/browser ./internal/server
fi
