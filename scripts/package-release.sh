#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-dev}"
OUT="$ROOT/dist/release"
SYSTEM="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$SYSTEM" in mingw*|msys*|cygwin*) SYSTEM=windows;; darwin) SYSTEM=darwin;; linux) SYSTEM=linux;; esac
case "$ARCH" in x86_64|amd64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; esac
NAME="paper-agent-${VERSION}-${SYSTEM}-${ARCH}"
STAGE="$OUT/$NAME"
mkdir -p "$STAGE"
for binary in paper-agent paper-agent-server feishu-adapter; do
  source="$ROOT/dist/bin/$binary"
  if [[ "$SYSTEM" == windows && -f "$source.exe" ]]; then source="$source.exe"; fi
  test -f "$source" || { echo "missing binary: $source" >&2; exit 1; }
  cp "$source" "$STAGE/$(basename "$source")"
done
cp "$ROOT/LICENSE" "$ROOT/README.md" "$ROOT/SECURITY.md" "$STAGE/"
if [[ "$SYSTEM" == windows ]] && command -v zip >/dev/null; then
  (cd "$OUT" && zip -qr "$NAME.zip" "$NAME")
  ARCHIVE="$OUT/$NAME.zip"
else
  tar -C "$OUT" -czf "$OUT/$NAME.tar.gz" "$NAME"
  ARCHIVE="$OUT/$NAME.tar.gz"
fi
if command -v sha256sum >/dev/null; then sha256sum "$ARCHIVE" >"$ARCHIVE.sha256"; else shasum -a 256 "$ARCHIVE" >"$ARCHIVE.sha256"; fi
if command -v cosign >/dev/null; then
  cosign sign-blob --yes --output-signature "$ARCHIVE.sig" --output-certificate "$ARCHIVE.pem" "$ARCHIVE"
fi
echo "$ARCHIVE"
