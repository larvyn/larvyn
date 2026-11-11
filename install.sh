#!/bin/sh
# Larvyn installer for Linux and macOS.
#
#   curl -fsSL https://larvyn.dev/install.sh | sh
#
# Options (environment variables):
#   LARVYN_VERSION=v0.1.0   install that release instead of the newest one
#   LARVYN_REPO=owner/repo  download releases from another GitHub repository
#   LARVYN_BIN_DIR=dir      where the `larvyn` command goes (default ~/.local/bin)
#   DO_NOT_TRACK=1          do not count this install (see the end of the script)
#
# Uninstall:
#   curl -fsSL https://larvyn.dev/install.sh | sh -s -- --uninstall
#
# Running it again updates Larvyn. Nothing needs sudo.

set -eu

REPO="${LARVYN_REPO:-larvyn/larvyn}"
BIN_DIR="${LARVYN_BIN_DIR:-$HOME/.local/bin}"
DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}"
MAC_APP="$HOME/Applications/Larvyn.app"

say() { printf '\033[1;35mlarvyn\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31mlarvyn\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required but was not found"; }

os=$(uname -s)
arch=$(uname -m)

uninstall() {
	case "$os" in
	Darwin)
		rm -rf "$MAC_APP"
		rm -f "$BIN_DIR/larvyn"
		;;
	*)
		rm -f "$BIN_DIR/larvyn" \
			"$DATA_DIR/applications/larvyn.desktop" \
			"$DATA_DIR/icons/hicolor/1024x1024/apps/larvyn.png"
		rm -rf "$DATA_DIR/larvyn"
		command -v update-desktop-database >/dev/null 2>&1 &&
			update-desktop-database -q "$DATA_DIR/applications" 2>/dev/null || true
		;;
	esac
	say "uninstalled. Your settings stay in ~/.config/larvyn; delete that folder to remove them too."
	exit 0
}

[ "${1:-}" = "--uninstall" ] && uninstall

need curl
need tar

case "$os" in
Linux)
	case "$arch" in
	x86_64 | amd64) asset="larvyn-linux-amd64.tar.gz" ;;
	*) fail "Linux $arch is not supported yet (only x86_64)" ;;
	esac
	;;
Darwin) asset="larvyn-darwin-universal.tar.gz" ;;
*) fail "$os is not supported by this script. On Windows run: irm https://larvyn.dev/install.ps1 | iex" ;;
esac

# The newest release, prereleases included, unless a version was asked for.
version="${LARVYN_VERSION:-}"
if [ -z "$version" ]; then
	version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=1" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$version" ] || fail "could not find a release in github.com/$REPO"
elif [ -z "${LARVYN_REPO:-}" ]; then
	# Releases up to beta.14 are only in the old distribution repo. Only a 404
	# switches to it; any other error tries this repo (download, no API limit).
	code=$(curl -sIL -o /dev/null -w '%{http_code}' "https://github.com/$REPO/releases/download/$version/checksums.txt" 2>/dev/null || true)
	[ "$code" = 404 ] && REPO=larvyn/releases
fi

base="https://github.com/$REPO/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $version ($asset)"
curl -fSL --progress-bar "$base/$asset" -o "$tmp/$asset" || fail "download failed: $base/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)
[ -n "$expected" ] || fail "$asset is missing from checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$asset" | cut -d ' ' -f 1)
else
	actual=$(shasum -a 256 "$tmp/$asset" | cut -d ' ' -f 1)
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$BIN_DIR"

if [ "$os" = "Darwin" ]; then
	mkdir -p "$HOME/Applications"
	rm -rf "$MAC_APP"
	mv "$tmp/larvyn.app" "$MAC_APP"
	ln -sf "$MAC_APP/Contents/MacOS/larvyn" "$BIN_DIR/larvyn"
	say "installed $version to $MAC_APP"
else
	install -m 755 "$tmp/larvyn" "$BIN_DIR/larvyn"

	# The icon theme has no 1024px folder, so a themed name would not resolve;
	# an absolute path works in every desktop. Older installs used hicolor.
	rm -f "$DATA_DIR/icons/hicolor/1024x1024/apps/larvyn.png"
	mkdir -p "$DATA_DIR/larvyn" "$DATA_DIR/applications"
	install -m 644 "$tmp/larvyn.png" "$DATA_DIR/larvyn/larvyn.png"
	cat >"$DATA_DIR/applications/larvyn.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Larvyn
Comment=The Laravel workbench
Exec=$BIN_DIR/larvyn
Icon=$DATA_DIR/larvyn/larvyn.png
StartupWMClass=larvyn
Categories=Development;
Terminal=false
EOF
	command -v update-desktop-database >/dev/null 2>&1 &&
		update-desktop-database -q "$DATA_DIR/applications" 2>/dev/null || true
	say "installed $version to $BIN_DIR/larvyn"

	# The window needs GTK 4 and WebKitGTK 6.0 at runtime.
	missing=$(ldd "$BIN_DIR/larvyn" 2>/dev/null | grep 'not found' | awk '{print $1}' | tr '\n' ' ')
	if [ -n "$missing" ]; then
		say "missing system libraries: $missing"
		if command -v apt-get >/dev/null 2>&1; then
			say "install them with: sudo apt install libgtk-4-1 libwebkitgtk-6.0-4"
		elif command -v dnf >/dev/null 2>&1; then
			say "install them with: sudo dnf install gtk4 webkitgtk6.0"
		elif command -v pacman >/dev/null 2>&1; then
			say "install them with: sudo pacman -S gtk4 webkitgtk-6.0"
		else
			say "install GTK 4 and WebKitGTK 6.0 with your package manager"
		fi
	fi
fi

# Count the install: GitHub counts downloads of each release file, and only the
# install scripts fetch installs.txt (the in-app updater does not), so its count
# is the number of installs. Nothing about you or your machine is sent; older
# releases have no such file, and a failure never matters.
if [ -z "${DO_NOT_TRACK:-}" ]; then
	curl -fsS --max-time 5 -o /dev/null "$base/installs.txt" 2>/dev/null || true
fi

case ":$PATH:" in
*":$BIN_DIR:"*) say "run it with: larvyn (or open Larvyn from your apps menu)" ;;
*) say "add $BIN_DIR to your PATH to run \`larvyn\` from a terminal" ;;
esac
