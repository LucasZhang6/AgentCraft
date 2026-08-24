#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
AGENT_PORT="${PAPER_AGENT_BROWSER_PORT:-18090}"
SITE_PORT="${ROADMAP_SITE_BROWSER_PORT:-4174}"
AGENT_SERVER_PID=""
SITE_SERVER_PID=""

cleanup() {
  if [[ -n "$AGENT_SERVER_PID" ]]; then kill "$AGENT_SERVER_PID" 2>/dev/null || true; wait "$AGENT_SERVER_PID" 2>/dev/null || true; fi
  if [[ -n "$SITE_SERVER_PID" ]]; then kill "$SITE_SERVER_PID" 2>/dev/null || true; wait "$SITE_SERVER_PID" 2>/dev/null || true; fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

"$ROOT/dist/bin/paper-agent-server" -provider demo -addr "127.0.0.1:$AGENT_PORT" -data-dir "$TMP_DIR/data" >"$TMP_DIR/server.log" 2>&1 &
AGENT_SERVER_PID=$!
for _ in {1..50}; do
  if curl -fsS "http://127.0.0.1:$AGENT_PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
PORT="$SITE_PORT" HOST="127.0.0.1" node "$ROOT/ai-agent-roadmap-site/server.mjs" >"$TMP_DIR/site.log" 2>&1 &
SITE_SERVER_PID=$!
for _ in {1..50}; do
  if curl -fsS "http://127.0.0.1:$SITE_PORT/" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
mkdir -p "$ROOT/artifacts/browser"
cd "$ROOT"
PAPER_AGENT_WEB_URL="http://127.0.0.1:$AGENT_PORT" node scripts/webui-regression.mjs
ROADMAP_SITE_URL="http://127.0.0.1:$SITE_PORT" node scripts/roadmap-site-regression.mjs
