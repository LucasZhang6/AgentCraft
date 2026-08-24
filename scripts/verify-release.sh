#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
shopt -s nullglob
checksums=("$ROOT"/dist/release/*.sha256)
((${#checksums[@]} > 0)) || { echo "no release checksums found" >&2; exit 1; }
for checksum in "${checksums[@]}"; do
  if command -v sha256sum >/dev/null; then (cd "$(dirname "$checksum")" && sha256sum -c "$(basename "$checksum")"); else (cd "$(dirname "$checksum")" && shasum -a 256 -c "$(basename "$checksum")"); fi
  archive="${checksum%.sha256}"
  if [[ -f "$archive.sig" && -f "$archive.pem" ]] && command -v cosign >/dev/null; then
    cosign verify-blob --certificate "$archive.pem" --signature "$archive.sig" \
      --certificate-identity-regexp 'https://github.com/.+/.github/workflows/release.yml@refs/tags/.+' \
      --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' "$archive"
  fi
done
