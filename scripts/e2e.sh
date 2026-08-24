#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
PORT="${PAPER_AGENT_E2E_PORT:-18089}"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

"$ROOT/dist/bin/paper-agent" -provider demo -data-dir "$TMP_DIR/cli" "解读 Agent Memory" >"$TMP_DIR/cli.out"
grep -q "问题背景" "$TMP_DIR/cli.out"
grep -q "累计任务成功率" "$TMP_DIR/cli.out"

"$ROOT/dist/bin/paper-agent-server" -provider demo -addr "127.0.0.1:$PORT" -data-dir "$TMP_DIR/server" >"$TMP_DIR/server.log" 2>&1 &
SERVER_PID=$!
for _ in {1..50}; do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.1
done
curl -fsS "http://127.0.0.1:$PORT/" | grep -q "Paper Agent"
curl -fsS -X POST "http://127.0.0.1:$PORT/api/agent/execute" \
  -H 'Content-Type: application/json' \
  -d '{"message":"解读 Tool Use","mode":"goal","async":false}' >"$TMP_DIR/execute.json"
grep -q '"success":true' "$TMP_DIR/execute.json"
grep -q '问题背景' "$TMP_DIR/execute.json"
echo "Paper Agent E2E passed"
