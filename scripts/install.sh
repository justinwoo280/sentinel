#!/bin/sh
# sentinel install bootstrap
#
# Downloads the sentinel binary from GitHub Releases, verifies its SHA-256
# against the release checksum list, installs it to /usr/local/bin, and
# launches the interactive installer.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/justinwoo280/sentinel/main/scripts/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --role master
#   curl -fsSL .../install.sh | sh -s -- --version v1.2.3
#
# Environment overrides:
#   SENTINEL_REPO      GitHub "owner/repo"       (default below)
#   SENTINEL_VERSION   release tag or "latest"   (default: latest)
#   SENTINEL_ROLE      agent|master              (default: prompt)
#   SENTINEL_BIN       install path              (default: /usr/local/bin/sentinel)

set -eu

# ---- configuration --------------------------------------------------------
REPO="${SENTINEL_REPO:-justinwoo280/sentinel}"
VERSION="${SENTINEL_VERSION:-latest}"
ROLE="${SENTINEL_ROLE:-}"
BIN_PATH="${SENTINEL_BIN:-/usr/local/bin/sentinel}"
CHECKSUMS_FILE="checksums.txt"

# ---- arg parsing ----------------------------------------------------------
while [ $# -gt 0 ]; do
	case "$1" in
	--role) ROLE="$2"; shift 2 ;;
	--role=*) ROLE="${1#*=}"; shift ;;
	--version) VERSION="$2"; shift 2 ;;
	--version=*) VERSION="${1#*=}"; shift ;;
	--repo) REPO="$2"; shift 2 ;;
	--repo=*) REPO="${1#*=}"; shift ;;
	--bin) BIN_PATH="$2"; shift 2 ;;
	-h|--help)
		echo "Usage: install.sh [--role agent|master] [--version vX.Y.Z] [--repo owner/repo]"
		exit 0 ;;
	*) echo "unknown arg: $1" >&2; exit 1 ;;
	esac
done

err() { echo "error: $*" >&2; exit 1; }
info() { echo ">> $*"; }

# ---- prerequisites --------------------------------------------------------
[ "$(id -u)" -eq 0 ] || err "must run as root (needed to write $BIN_PATH and /etc/sentinel)"

if command -v curl >/dev/null 2>&1; then
	DLO="curl -fsSL -o"
elif command -v wget >/dev/null 2>&1; then
	DLO="wget -qO"
else
	err "need curl or wget"
fi

if command -v sha256sum >/dev/null 2>&1; then
	SHACMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
	SHACMD="shasum -a 256"
else
	err "need sha256sum or shasum for checksum verification"
fi

# ---- detect OS/arch -------------------------------------------------------
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
[ "$OS" = "linux" ] || err "only linux is supported (got $OS)"

MACHINE="$(uname -m)"
case "$MACHINE" in
	x86_64|amd64) ARCH="amd64" ;;
	aarch64|arm64) ARCH="arm64" ;;
	*) err "unsupported architecture: $MACHINE" ;;
esac

ASSET="sentinel-${OS}-${ARCH}"
info "platform: ${OS}/${ARCH}  asset: ${ASSET}"

# ---- resolve version + URLs ----------------------------------------------
if [ "$VERSION" = "latest" ]; then
	BASE="https://github.com/${REPO}/releases/latest/download"
else
	BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi
BIN_URL="${BASE}/${ASSET}"
SUM_URL="${BASE}/${CHECKSUMS_FILE}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# ---- download binary + checksums -----------------------------------------
info "downloading binary: $BIN_URL"
$DLO "$TMP/$ASSET" "$BIN_URL" || err "failed to download binary"

info "downloading checksums: $SUM_URL"
$DLO "$TMP/$CHECKSUMS_FILE" "$SUM_URL" || err "failed to download checksum list"

# ---- verify SHA-256 -------------------------------------------------------
EXPECTED="$(grep " ${ASSET}\$" "$TMP/$CHECKSUMS_FILE" | awk '{print $1}' | head -n1)"
[ -n "$EXPECTED" ] || err "no checksum entry for ${ASSET} in ${CHECKSUMS_FILE}"

ACTUAL="$($SHACMD "$TMP/$ASSET" | awk '{print $1}')"
if [ "$EXPECTED" != "$ACTUAL" ]; then
	err "checksum MISMATCH for ${ASSET}
  expected: ${EXPECTED}
  actual:   ${ACTUAL}
  refusing to install a corrupted or tampered binary"
fi
info "checksum verified: ${ACTUAL}"

# ---- install --------------------------------------------------------------
install -m 0755 "$TMP/$ASSET" "$BIN_PATH" || err "failed to install to $BIN_PATH"
info "installed: $BIN_PATH ($("$BIN_PATH" version 2>/dev/null || echo '?'))"

mkdir -p /etc/sentinel /var/lib/sentinel /var/log/sentinel
chmod 0755 /etc/sentinel /var/log/sentinel
chmod 0700 /var/lib/sentinel

# ---- launch interactive installer ----------------------------------------
# When this script itself is run as "curl ... | sh", sh's stdin is the
# pipe carrying this script's own source. By the time we get here, curl
# has already sent everything and closed, so stdin is exhausted (EOF) —
# any interactive prompt in the installer below would silently read EOF
# and fall through to defaults instead of asking the user anything.
#
# Fix: if stdin isn't already a terminal, reopen it from the controlling
# terminal (/dev/tty) before exec'ing the installer, so prompts work
# correctly regardless of whether this script was invoked via a pipe or
# "sh -c \"\$(curl ...)\"". Note: testing /dev/tty must NOT use a bare
# "exec N</dev/tty" here — POSIX shells (dash in particular) treat a
# failing redirection on a bare exec as fatal to the whole script, even
# inside an "if" condition. Testing with an ordinary command first avoids
# that trap.
info "launching interactive installer..."
if [ -t 0 ]; then
	: # stdin is already a real terminal — nothing to do
elif (true < /dev/tty) 2>/dev/null; then
	exec < /dev/tty
else
	echo "" >&2
	echo "warning: no controlling terminal detected; the interactive installer" >&2
	echo "below requires one to answer prompts (region, master address, etc.)." >&2
	echo "If this hangs or fails, re-run with:" >&2
	echo "  sh -c \"\$(curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh)\"" >&2
	echo "" >&2
fi

if [ -n "$ROLE" ]; then
	exec "$BIN_PATH" install --role "$ROLE"
else
	exec "$BIN_PATH" install
fi
