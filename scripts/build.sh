#!/usr/bin/env bash
# Build/test helper for libi-print-agent. Uses the local `go` toolchain when
# available (Windows dev box, CI with Go pre-installed); otherwise falls back
# to the official Docker image, since this Mac intentionally has no local Go
# install. Named Docker volumes cache the module and build caches across runs.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_IMAGE="${GO_IMAGE:-golang:1.23}"
MOD_CACHE_VOLUME="${MOD_CACHE_VOLUME:-libi-print-agent-gomodcache}"
BUILD_CACHE_VOLUME="${BUILD_CACHE_VOLUME:-libi-print-agent-gobuildcache}"

go_cmd() {
  local cgo="${CGO_ENABLED:-0}"
  if command -v go >/dev/null 2>&1; then
    CGO_ENABLED="$cgo" go "$@"
  else
    docker run --rm \
      -v "$ROOT_DIR":/src \
      -v "$MOD_CACHE_VOLUME":/go/pkg/mod \
      -v "$BUILD_CACHE_VOLUME":/root/.cache/go-build \
      -w /src \
      -e CGO_ENABLED="$cgo" \
      -e GOOS \
      -e GOARCH \
      "$GO_IMAGE" go "$@"
  fi
}

cmd="${1:-build-windows}"
[ $# -gt 0 ] && shift || true

case "$cmd" in
  tidy)
    go_cmd mod tidy
    ;;
  vet)
    go_cmd vet ./...
    ;;
  test)
    go_cmd test ./...
    ;;
  build-windows)
    # -H=windowsgui builds a GUI-subsystem exe (no console flash on launch,
    # the agent is tray-only). LIBI_PRINT_AGENT_VERSION/API_BASE/
    # REQUIRE_SIGNATURE are optional build-time overrides for the three
    # -X-settable vars in cmd/libi-print-agent/main.go; the real release
    # pipeline (task 8.5) is expected to set VERSION and API_BASE, and to
    # leave REQUIRE_SIGNATURE at its "true" default except for an unsigned
    # v0.x test build. Unset, a local dev build stays "dev"/empty-API-base/
    # signature-required, same as before this flag support existed.
    #
    # -buildvcs=false: the Docker-image build path (this Mac; also the
    # golang:1.23 container CI would fall back to if Go were ever missing)
    # bind-mounts the repo into a container running as a different uid than
    # the host, which `git`/`go build`'s VCS stamping rejects as "dubious
    # ownership" (exit status 128). The build's version already comes from
    # -X main.version above, so VCS stamping adds nothing worth fighting
    # for; disabled unconditionally so local Docker builds and CI behave
    # the same way regardless of which `go` (local or containerized) ran.
    mkdir -p "$ROOT_DIR/dist"
    ldflags="-H=windowsgui"
    if [ -n "${LIBI_PRINT_AGENT_VERSION:-}" ]; then
      ldflags="$ldflags -X main.version=${LIBI_PRINT_AGENT_VERSION}"
    fi
    if [ -n "${LIBI_PRINT_AGENT_API_BASE:-}" ]; then
      ldflags="$ldflags -X main.defaultAPIBase=${LIBI_PRINT_AGENT_API_BASE}"
    fi
    if [ -n "${LIBI_PRINT_AGENT_REQUIRE_SIGNATURE:-}" ]; then
      ldflags="$ldflags -X main.requireSignature=${LIBI_PRINT_AGENT_REQUIRE_SIGNATURE}"
    fi
    GOOS=windows GOARCH=amd64 go_cmd build -buildvcs=false -ldflags "$ldflags" -o dist/libi-print-agent.exe ./cmd/libi-print-agent
    ;;
  vet-windows)
    # Type-checks the Windows-only build tags (DPAPI, winspool) that `vet`/`test`
    # never touch on a non-Windows machine. Cannot execute them, only compile.
    GOOS=windows GOARCH=amd64 go_cmd vet ./...
    ;;
  *)
    echo "usage: $(basename "$0") {tidy|vet|test|build-windows|vet-windows}" >&2
    exit 1
    ;;
esac
