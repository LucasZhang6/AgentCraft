#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
for file in LICENSE README.md SECURITY.md CONTRIBUTING.md CODE_OF_CONDUCT.md; do
  test -s "$file" || { echo "missing required file: $file" >&2; exit 1; }
done
if rg -n --hidden --glob '!node_modules/**' --glob '!dist/**' --glob '!.git/**' \
  '(sk-[A-Za-z0-9_-]{32,}|AKIA[0-9A-Z]{16}|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----)' .; then
  echo "possible secret found" >&2
  exit 1
fi
if rg -n --pcre2 'uses:\s+[^\s]+@(?![0-9a-f]{40}(?:\s|$))[^\s#]+' .github/workflows; then
  echo "GitHub Actions must be pinned to full commit SHAs" >&2
  exit 1
fi
unformatted="$(gofmt -l examples/your-agent)"
test -z "$unformatted" || { echo "gofmt required:" >&2; echo "$unformatted" >&2; exit 1; }
GOCACHE="$ROOT/.gocache" go vet ./examples/your-agent/...
GOCACHE="$ROOT/.gocache" go test ./examples/your-agent/...
npm run docs:locales
npm --prefix ai-agent-roadmap-site test
echo "Open-source checks passed"
