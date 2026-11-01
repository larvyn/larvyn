#!/usr/bin/env bash
# Builds bin/larvyn on macOS without task or wails3, with the same
# deployment target as build/darwin/Taskfile.yml, and runs it with --run.
#
#   scripts/build-macos.sh          # build
#   scripts/build-macos.sh --run    # build, then start it
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ "$(uname)" != "Darwin" ]]; then
  echo "macOS only: use wails3 build elsewhere" >&2
  exit 1
fi

# Keep in sync with build/darwin/Taskfile.yml. Without it the cgo objects
# target the SDK's macOS version while Go links for 11.0, and ld warns
# "object file was built for newer 'macOS' version".
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"
export CGO_ENABLED=1
export CGO_CFLAGS="${CGO_CFLAGS:--mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET}"
export CGO_LDFLAGS="${CGO_LDFLAGS:--mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET}"
# Apple's clang and SDK: a Homebrew llvm first on PATH cannot find the
# system headers or libresolv.
export CC="${CC:-/usr/bin/clang}"
export SDKROOT="${SDKROOT:-$(xcrun --show-sdk-path)}"

if [[ ! -f frontend/dist/index.html || "${REBUILD_FRONTEND:-}" == 1 ]]; then
  (cd frontend && { [[ -d node_modules ]] || npm ci; } && npm run build)
fi

go build -buildvcs=false -gcflags=all="-l" -o bin/larvyn .
echo "built bin/larvyn"

# WebKit keeps localStorage in ~/Library/WebKit/<app>. If that folder is not
# yours (some installers leave it owned by root), the window works but
# nothing it saves survives a restart.
webkit="$HOME/Library/WebKit"
if [[ -d "$webkit" && ! -w "$webkit" ]]; then
  echo "warning: $webkit is not writable by $(id -un); the app cannot keep its settings." >&2
  echo "         fix with: sudo chown -R $(id -un):staff \"$webkit\"" >&2
fi

if [[ "${1:-}" == "--run" ]]; then
  exec ./bin/larvyn
fi
