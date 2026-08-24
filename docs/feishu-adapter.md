# Feishu Adapter

[English](feishu-adapter.md) | [简体中文](feishu-adapter.zh-CN.md)

`feishu-adapter` is an independent sidecar between Feishu callbacks and the
Paper Agent HTTP API. It owns Feishu credentials, callback verification, card
rendering, event deduplication, and durable `chat_id -> session_id` mapping. The
Agent Server continues to own the LLM provider, tool permissions, goals, plans,
sessions, and Agent Loop.

## Supported Behavior

- `/feishu/events` message callbacks and `/feishu/card-callback` approvals
- durable Sessions keyed by `tenant_key + chat_id`
- text, post/rich text, replies and quoted messages
- JPEG, PNG, GIF, and WebP images
- asynchronous task submission, polling, cancellation, and final replies
- interactive approval cards for `pending_approval`
- `/help`, `/new`, `/status`, and `/cancel`
- group tasks only after an explicit bot mention; `@all` alone is ignored

The current adapter uses HTTP callbacks. Feishu long connections and encrypted
callback payloads are not implemented yet. Paper Agent also does not copy the
scheduler notifier from the reference Verdent implementation because it has no
scheduled-task subsystem.

## Build

From the repository root:

```bash
make build
```

Artifacts:

```text
dist/bin/paper-agent
dist/bin/paper-agent-server
dist/bin/feishu-adapter
```

## Start Paper Agent Server

The LLM API key belongs only to the Agent Server:

```bash
export OPENAI_API_KEY="..."
export OPENAI_BASE_URL="https://your-compatible-provider.example/v1"
export OPENAI_MODEL="your-model-id"
export PAPER_AGENT_ACCESS_ID="replace-with-a-random-access-id"

dist/bin/paper-agent-server -provider openai
```

The default address is `127.0.0.1:18080`. `PAPER_AGENT_ACCESS_ID` enables a
login exchange for a Bearer token. An unauthenticated server is appropriate only
for a controlled local environment.

## Start the Sidecar

```bash
export FEISHU_APP_ID="cli_xxx"
export FEISHU_APP_SECRET="..."
export FEISHU_VERIFICATION_TOKEN="..."
export PAPER_AGENT_BASE_URL="http://127.0.0.1:18080"
export PAPER_AGENT_ACCESS_ID="replace-with-the-same-access-id"

dist/bin/feishu-adapter --addr :18790 --mode agent
```

| Flag | Environment | Default | Purpose |
| --- | --- | --- | --- |
| `--addr` | `FEISHU_ADAPTER_ADDR` | `:18790` | callback listen address |
| `--feishu-app-id` | `FEISHU_APP_ID` | | Feishu application ID |
| `--feishu-app-secret` | `FEISHU_APP_SECRET` | | Feishu application secret |
| `--feishu-verification-token` | `FEISHU_VERIFICATION_TOKEN` | | callback token; mismatch returns 401 |
| `--paper-agent-url` | `PAPER_AGENT_BASE_URL` | `http://127.0.0.1:18080` | Paper Agent API |
| `--paper-agent-access-id` | `PAPER_AGENT_ACCESS_ID` | | Agent login credential |
| `--db` | `FEISHU_ADAPTER_DB` | `~/.config/paper-agent/feishu-adapter.db` | mapping SQLite database |
| `--poll-interval` | `FEISHU_ADAPTER_POLL_INTERVAL` | `2s` | task polling interval |
| `--poll-timeout` | `FEISHU_ADAPTER_POLL_TIMEOUT` | `30m` | per-task polling deadline |
| `--auto-approve` | `PAPER_AGENT_FEISHU_AUTO_APPROVE` | `false` | trusted automation only |

## Feishu Console

Enable a bot and configure:

- event callback: `https://<public-host>/feishu/events`
- card callback: `https://<public-host>/feishu/card-callback`
- event: `im.message.receive_v1`
- permissions to receive and send messages
- card permissions when interactive approval is enabled
- message and resource read permissions for quoted messages and images

Expose the callback through an authenticated, rate-limited HTTPS reverse proxy
or tunnel. Keep Paper Agent Server on loopback when both processes share a host.

## Sessions, Quotes, and Images

Normal messages submit asynchronous tasks. Messages from the same Feishu chat
reuse a Session until `/new` is used. For replies, the adapter reads `parent_id`
before `root_id`, extracts visible quoted content, and combines it with the new
message. Card payload internals are not forwarded as user-visible quote text.

Quoted images are ordered before current-message images. A request may contain
at most four images, each at most 8 MiB and at most 20 MiB total. A failed
download or invalid format returns an explicit error instead of silently
submitting an incomplete prompt.

## Approval and Trust Boundary

When a tool waits for approval, the sidecar sends an Approve/Reject card. A
click calls `/api/agent/approve`, then updates the original card and disables its
buttons. Final results are sent as cards with a text fallback.

Keep auto-approval disabled in production. Feishu is an input surface, not an
authorization layer: tool schemas, allowlists, risk, timeout, output budget, and
the final permission decision remain in Paper Agent Runtime. Long replies are
truncated for Feishu while complete Session history remains in local SQLite.
