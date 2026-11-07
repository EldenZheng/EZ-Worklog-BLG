#!/usr/bin/env bash
# Builds the app on macOS or Linux with the same patched Fyne as build.ps1.
#
#   bash build.sh [--test] [output]
#
# output defaults to ../Worklog, beside the data folder. Publishing a release
# stays with build.ps1 and .github/workflows/macos.yml.
set -euo pipefail
cd "$(dirname "$0")"

run_tests=false
if [ "${1:-}" = "--test" ]; then
    run_tests=true
    shift
fi
output="${1:-../Worklog}"

version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
go run ./tools/patchfyne
build_mod=.build/cursors/worklog.mod
if [ "$run_tests" = true ]; then
    go test -modfile "$build_mod" -tags cursors ./...
fi
mkdir -p "$(dirname "$output")"
go build -modfile "$build_mod" -ldflags "-s -w -X worklog/internal/shared/backend.Version=$version" -o "$output" .
