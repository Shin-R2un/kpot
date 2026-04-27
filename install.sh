#!/usr/bin/env bash
# kpot installer for Linux and macOS.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/Shin-R2un/kpot/main/install.sh | bash
#
# Environment overrides:
#   KPOT_VERSION       — pin to a specific tag (e.g. v0.5.0). Default: latest release.
#   KPOT_INSTALL_DIR   — install destination. Default: /usr/local/bin (uses sudo if needed).

set -euo pipefail

REPO="Shin-R2un/kpot"
INSTALL_DIR="${KPOT_INSTALL_DIR:-/usr/local/bin}"

err() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m→\033[0m %s\n' "$*"; }
ok() { printf '\033[32m✓\033[0m %s\n' "$*"; }

# --- Detect OS ---
case "$(uname -s)" in
  Linux*)  os=linux ;;
  Darwin*) os=darwin ;;
  *) err "unsupported OS: $(uname -s) (kpot ships linux and darwin builds)" ;;
esac

# --- Detect arch ---
case "$(uname -m)" in
  x86_64|amd64)   arch=amd64 ;;
  arm64|aarch64)  arch=arm64 ;;
  *) err "unsupported arch: $(uname -m) (kpot ships amd64 and arm64)" ;;
esac

# --- Resolve version ---
ver="${KPOT_VERSION:-}"
if [ -z "$ver" ]; then
  info "Resolving latest release tag..."
  api_resp=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest") \
    || err "could not reach GitHub API"
  ver=$(printf '%s\n' "$api_resp" | awk -F'"' '/"tag_name"/{print $4; exit}')
  [ -n "$ver" ] || err "could not parse latest tag from GitHub API response"
fi
ver_no_v="${ver#v}"

url="https://github.com/${REPO}/releases/download/${ver}/kpot_${ver_no_v}_${os}_${arch}.tar.gz"
sums_url="https://github.com/${REPO}/releases/download/${ver}/checksums.txt"

info "Installing kpot ${ver} (${os}/${arch}) → ${INSTALL_DIR}/kpot"

# --- Download & verify ---
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$url"      -o "$tmp/kpot.tar.gz" || err "download failed: $url"
curl -fsSL "$sums_url" -o "$tmp/checksums.txt" || err "checksum download failed: $sums_url"

archive_name="kpot_${ver_no_v}_${os}_${arch}.tar.gz"
expected=$(grep " ${archive_name}\$" "$tmp/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || err "no checksum entry for ${archive_name} in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/kpot.tar.gz" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp/kpot.tar.gz" | awk '{print $1}')
else
  err "neither sha256sum nor shasum found — cannot verify integrity"
fi
[ "$actual" = "$expected" ] || err "checksum mismatch (expected $expected, got $actual)"
ok "checksum verified ($expected)"

tar -xzf "$tmp/kpot.tar.gz" -C "$tmp" kpot

# --- Install (sudo if needed) ---
# Create the dir first if it doesn't exist (best effort, no sudo).
[ -d "$INSTALL_DIR" ] || mkdir -p "$INSTALL_DIR" 2>/dev/null || true

if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$tmp/kpot" "$INSTALL_DIR/kpot"
elif [ -d "$INSTALL_DIR" ]; then
  info "Need sudo to write to ${INSTALL_DIR} (override with KPOT_INSTALL_DIR=\$HOME/.local/bin)"
  sudo install -m 0755 "$tmp/kpot" "$INSTALL_DIR/kpot"
else
  err "could not create ${INSTALL_DIR} (no write permission, sudo not attempted)"
fi

ok "kpot ${ver} installed to ${INSTALL_DIR}/kpot"
"${INSTALL_DIR}/kpot" version
