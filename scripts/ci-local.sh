#!/usr/bin/env bash
# Runs the CI checks of .github/workflows/test.yml on this machine, for when
# GitHub Actions can't (no minutes left, an outage) or to check before
# pushing:
#
#   scripts/ci-local.sh            # Linux (in Docker) and this Mac
#   scripts/ci-local.sh linux      # only the Linux jobs: go + frontend
#   scripts/ci-local.sh mac        # only the Go job on this Mac
#
# Linux runs the workflow itself with act (https://nektosact.com, 0.2.80 or
# newer, for node24 actions) in Docker. The Mac job runs the same steps
# natively on a clean checkout of HEAD. Windows can't run here.
set -euo pipefail
cd "$(dirname "$0")/.."
what=${1:-all}
fail=0

linux() {
	command -v act >/dev/null || { echo "act is not installed: brew install act"; return 1; }
	docker info >/dev/null 2>&1 || { echo "Docker is not running"; return 1; }
	local arch
	arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
	for job in frontend go; do
		echo "== Linux: $job"
		# --init: a real PID 1 reaps orphans, as on GitHub's runners. Without
		# it, killed processes stay as zombies and the process-tree tests
		# think they survived.
		act pull_request -W .github/workflows/test.yml -j "$job" --matrix os:ubuntu-24.04 \
			-P ubuntu-24.04=catthehacker/ubuntu:act-24.04 --container-architecture "linux/$arch" \
			--container-options "--init" || fail=1
	done
}

mac() {
	[ "$(uname)" = Darwin ] || { echo "not a Mac"; return 0; }
	echo "== macOS: go"
	local tree
	tree=$(mktemp -d)
	git worktree add -q --detach "$tree" HEAD
	# Each step exits on failure itself: the "|| fail=1" after the subshell
	# turns set -e off inside it.
	(
		cd "$tree" || exit 1
		# main.go embeds frontend/dist; the tests only need something there.
		mkdir -p frontend/dist && echo '<!doctype html>' >frontend/dist/index.html || exit 1
		# Without the Command Line Tools, cgo needs Xcode's clang and SDK.
		export CC="${CC:-/usr/bin/clang}" SDKROOT="${SDKROOT:-$(xcrun --show-sdk-path)}"
		unformatted=$(gofmt -l $(git ls-files '*.go' ':!build/'))
		if [ -n "$unformatted" ]; then
			echo "Run gofmt -w on:"; echo "$unformatted"; exit 1
		fi
		go vet ./... || exit 1
		go test -race ./... || exit 1
	) || fail=1
	git worktree remove --force "$tree"
}

case $what in
all) linux || fail=1; mac || fail=1 ;;
linux) linux || fail=1 ;;
mac)
	# Asked for explicitly, a missing Mac is a failure; "all" skips it.
	if [ "$(uname)" = Darwin ]; then mac || fail=1; else echo "mac needs a Mac"; fail=1; fi ;;
*) echo "usage: $0 [all|linux|mac]"; exit 2 ;;
esac
if [ $fail -ne 0 ]; then echo "== CI failed"; exit 1; fi
echo "== CI passed"
