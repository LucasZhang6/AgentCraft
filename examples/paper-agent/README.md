# Paper Agent Runtime

[English](README.md) | [简体中文](README.zh-CN.md)

Paper Agent is a Go agent runtime whose engineering boundaries are derived from
the research covered by this repository. It supports deterministic offline
regression and real OpenAI-compatible execution, while keeping plans, tools,
permissions, sessions, goals, memory, evaluation, and observability under host
control.

![Paper Agent goal, planning, tool, observation, evaluation, and memory loop](../../assets/architecture/minimal-agent-loop.png)

## Build and Run

From the repository root:

```bash
make build
```

The binaries are placed in `dist/bin/`.

Run the persistent readline CLI:

```bash
dist/bin/paper-agent -interactive -provider demo
```

It supports persistent history, completion, `Ctrl+R` search, and these commands:

```text
/session
/memory
/model
/goal pause|resume|clear|status
/plan show|list|resume|accept
/todo
/paste
/clear
/exit
```

`Ctrl+C` cancels the active task without deleting the Session. Run the
full-screen terminal interface with:

```bash
dist/bin/paper-agent -tui -provider demo
```

One-shot offline execution is also available:

```bash
npm run agent -- "Explain a representative Tool Use paper"
```

## Real Model Provider

Credentials are read from environment variables only:

```bash
export OPENAI_API_KEY="..."
export OPENAI_MODEL="your-model-id"

dist/bin/paper-agent \
  -provider openai \
  -fallback-models "fallback-a,fallback-b" \
  "Explain agent planning and map it to this runtime"
```

Set `OPENAI_BASE_URL` or `-base-url` for an OpenAI-compatible Responses API
gateway. Model IDs are configuration rather than hard-coded project defaults.

### Provider Behavior

`internal/provider/openai.go` provides:

- Bearer authentication without persisting or logging the API key
- custom base URLs
- SSE incremental output
- bounded backoff for 429 and 5xx responses
- an ordered primary/fallback model chain
- `prompt_cache_key` and cache read/create token accounting
- request deadlines and response-size limits
- input, output, total, and cached token usage
- `store=false`, because the local runtime owns state

The final report can stream to the UI, while controller JSON stays internal.
The official OpenAI endpoint receives `max_output_tokens`. Custom gateways omit
that optional field for compatibility; configure an output limit on the gateway
as well as relying on the local response-body bound.

`internal/model/openai.go` asks the provider for two structured objects: a plan
and the next controller decision. Text must parse into the expected schema, then
still pass the local plan, tool, permission, and evaluation gates.

## HTTP and Web UI

Start the asynchronous API server:

```bash
dist/bin/paper-agent-server -provider openai
```

Open `http://127.0.0.1:18080/`. Task states include `running`,
`pending_approval`, `completed`, `failed`, and `canceled`. Reusing a
`session_id` continues the same conversation.

Important endpoints include:

```text
/api/agent/execute
/api/agent/status
/api/agent/events
/api/agent/cancel
/api/agent/approve
/api/session/status
/api/goal/status
/api/goal/action
/api/plan/latest
/api/plan/accept
```

Set `PAPER_AGENT_ACCESS_ID` to require a login exchange for a Bearer token. The
default loopback-only configuration is intended for local use, not direct
public exposure.

## Feishu Adapter

`feishu-adapter` is a separate sidecar over the HTTP API. It supports text,
rich-text, quoted messages, images, explicit group mentions, event deduplication,
durable chat-to-session mapping, polling, cancellation, and approval cards.

The sidecar owns Feishu credentials; the Agent Server owns the LLM key and all
runtime permissions. See the [Feishu adapter guide](../../docs/feishu-adapter.md).
Paper Agent does not copy the scheduler notifier from the reference Verdent
implementation because it has no scheduled-task subsystem.

## Agent and Goal Loops

Execution has two levels:

1. A Goal turn runs `Decide -> Tool -> Observation` for at most `max-steps`.
2. If the evaluator rejects the candidate result, the persisted Goal compresses
   old observations and starts another turn.

`goal-turns=0` and `token-budget=0` mean unlimited. Pause, cancellation,
evaluator success, explicit limits, and unrecoverable failures can still stop
the Goal.

The model-generated plan must pass deterministic validation before execution:
unique step IDs, existing dependencies, an acyclic graph, available tools,
non-empty success criteria, and valid initial state. A model output does not
grant execution authority.

## Tool Catalog and Policy

Built-in tools include:

```text
search_papers        read_paper_card
file_read            file_write            file_edit
list_dir             glob                  grep
bash                 web_fetch             web_search
clarification        subagent
```

Tools have `read`, `write`, or `dangerous` risk. Write and dangerous actions
require approval by default. In a non-interactive process, `approval=ask` fails
closed until the HTTP or Feishu approval flow responds.

Every call is subject to:

- registration and allowlist lookup
- argument schema validation
- risk and approval policy
- timeout and context cancellation
- serialized output byte budget
- tool result and failure metrics
- an observation returned to the next model turn

Large string results are truncated; oversized structured results are rejected
instead of being silently corrupted. File tools reject workspace escape and
symbolic-link bypass. Web tools reject private, loopback, and link-local targets
and unsafe redirects.

### Plugins and MCP

`.paper-agent/plugins.json` can register command plugins or MCP stdio servers:

```json
{
  "version": 1,
  "plugins": [
    {
      "name": "lookup",
      "command": ["./bin/lookup"],
      "input_schema": {
        "type": "object",
        "properties": { "query": { "type": "string" } },
        "required": ["query"]
      }
    }
  ],
  "mcp_servers": [
    { "name": "workspace", "command": ["npx", "-y", "@example/mcp-server"] }
  ]
}
```

External tools receive a `plugin__...` name and default to dangerous. Their
schema, timeout, bounded output, and approval pass through the same registry as
built-in tools. Only register commands and MCP servers you trust.

## Session Persistence and Compaction

Complete messages and structured runtime events live in
`.agent-data/sessions.db`. Sessions can be listed, inspected, titled, and
forked. Compaction never deletes the stored messages; it only changes the next
model-facing view.

Default compaction triggers are a rendered context above 400 KB or a prior input
above 120K tokens:

- keep the most recent four user turns by default
- L1 micro-compacts repeated and oversized old messages
- L2 always adds a deterministic digest of goals, conclusions, failures, and
  URLs
- L3 asynchronously prewarms a semantic summary at 75% of the threshold in
  OpenAI mode
- L3 has a 12-second hard timeout and falls back to L2 without blocking the
  current request
- a context-length provider error retries with one recent turn, then only the
  current turn

Summary calls, input/output tokens, and latency are stored in the Session row
and shown by `/session` or `/api/session/status`. Set
`PAPER_AGENT_SESSION_LLM_SUMMARY=0` to disable L3 for offline, quota-limited, or
fault-injection runs.

The Agent Loop separately keeps `recentObservations` and a Goal-local
`contextSummary`. That working-memory layer is not the same as cross-user-turn
Session compaction.

## Goal Persistence

Goals live in `.agent-data/goals.db`, bound to but separate from Sessions. A
Goal stores its objective, state, token use, turn count, transition history, and
auto-resume setting. CLI and HTTP support pause, resume, clear, and status.

Restarting the process continues the same Goal rather than wrapping the old
objective in a new prompt. Default token and turn limits are unlimited, while
each turn remains bounded by `max-steps`.

## Persistent Planning

Plans live in `.agent-data/plans.db`. Steps retain dependencies, role, status,
attempts, evidence, and acceptance checks. The scheduler only dispatches ready
steps and can run independent researcher/reviewer roles concurrently.

Deterministic verifiers inspect file and output evidence. A `human` check moves
the step to `awaiting_acceptance`, where CLI or HTTP must explicitly accept it.
This makes planning execution state, not decorative text.

## SQLite Memory

Long-term memory lives in `.agent-data/memory.db`. Each record includes:

- an arbitrary `scope`
- source and confidence
- `active` or `archived` lifecycle state
- creation, update, and last-used timestamps
- a unique `(scope, key)` for updates

Retrieval applies both result-count and byte budgets, then ranks by scope,
keywords, confidence, and recency. The default runtime queries `user`, `project`,
and `learning-preference` scopes. Obvious API keys, tokens, and passwords are
rejected before write.

Memory is evidence, not authorization. Web or tool content that reaches memory
remains untrusted on future retrieval.

## Metrics and Trajectories

Each run writes a redacted JSONL trace to
`.agent-data/runs/<run-id>.jsonl`. Cross-run metrics live in
`.agent-data/metrics.db`; the CLI reports cumulative task success.

`metrics_recorded` includes:

```text
success
duration_ms
llm_calls
input_tokens / output_tokens / total_tokens
cache_read_input_tokens / cache_creation_input_tokens
tool_calls / tool_failures / tool_duration_ms
human_approval_requests
context_compactions
goal_turns
```

Session L3 summarization is an independent model operation and has separate
usage fields in `sessions.db`, so its cost is not hidden.

## Theory-to-Code Map

| Runtime mechanism | Research connection |
| --- | --- |
| `Decide -> Tool -> Observation` | ReAct-style feedback between reasoning and environment |
| model tool selection plus host authorization | [Toolformer](../../ai-agent-roadmap-site/assets/pdfs/toolformer.pdf) |
| validated and recoverable DAG | [Understanding the Planning of LLM Agents](../../ai-agent-roadmap-site/assets/pdfs/understanding-the-planning-of-llm-agents.pdf) |
| scoped lifecycle memory and retrieval budgets | [Memory in the LLM Era](../../ai-agent-roadmap-site/assets/pdfs/memory-in-the-llm-era.pdf) and [A-MEM](../../ai-agent-roadmap-site/assets/pdfs/a-mem-agentic-memory.pdf) |
| recent context, digest, semantic summary | [HiAgent](../../ai-agent-roadmap-site/assets/pdfs/hiagent.pdf) |
| provenance without implicit trust | [AgentPoison](../../ai-agent-roadmap-site/assets/pdfs/agentpoison.pdf) |
| success, tokens, latency, tools, and compression metrics | [Toward Efficient Agents](../../ai-agent-roadmap-site/assets/pdfs/toward-efficient-agents.pdf) |

The papers provide design principles. The code adds deterministic constraints:
the model suggests a plan, but code checks the DAG; it requests a tool, but the
registry checks authority; it proposes completion, but an evaluator accepts it.

## Code Map

| Path | Responsibility |
| --- | --- |
| `cmd/paper-agent/main.go` | CLI, readline/TUI mode, provider flags, approval, and metrics |
| `cmd/paper-agent-server/main.go` | async HTTP tasks, Session, cancellation, and approval |
| `cmd/feishu-adapter/main.go` | Feishu callback sidecar |
| `internal/agent/agent.go` | ReAct, Goal continuation, observation compaction, stop conditions |
| `internal/provider/openai.go` | OpenAI-compatible Responses transport |
| `internal/model/` | deterministic and real planner/controller models, session summarizer |
| `internal/tools/` | registry, paper, file, shell, web, clarification, and subagent tools |
| `internal/plugin/` | command plugins and MCP stdio integration |
| `internal/session/` | messages/events, L1/L2/L3 views, recovery, status and fork |
| `internal/goal/` | independent Goal lifecycle |
| `internal/planning/` | validation, SQLite plan store, scheduler, verifier and acceptance |
| `internal/memory/` | SQLite memory lifecycle and budgeted retrieval |
| `internal/metrics/` | run metrics and cumulative task success |
| `internal/evaluator/` | deterministic report acceptance |
| `internal/trajectory/` | concurrent redacted JSONL trace |
| `internal/server/` | task state machine and HTTP protocol |
| `internal/input/`, `render/`, `tui/`, `ui/` | terminal input and presentation |
| `internal/app/factory.go` | runtime composition and resource lifecycle |

## Key Flags

```text
-provider demo|openai
-model <model-id>
-fallback-models <id,id>
-base-url <openai-compatible-base-url>
-max-steps 6
-goal-turns 0
-token-budget 0
-context-observations 4
-max-output-tokens 4096
-tool-timeout 30s
-tool-output-bytes 65536
-approval ask|deny|allow
-data-dir .agent-data
-interactive
-tui
-session-id <id>
-session-trigger-bytes 400000
-session-trigger-tokens 120000
-session-recent-turns 4
```

`approval=allow` is only appropriate inside a controlled automation boundary.

## Verification

The runtime requires Go 1.24.2, CGO, and a C compiler. SQLite uses
`github.com/mattn/go-sqlite3`.

```bash
GOCACHE="$PWD/.gocache" go test ./...
```

From the repository root:

```bash
make e2e
make browser-regression
make open-source-check
make release-snapshot
make verify-release
```

GitHub Actions build and test Linux, macOS, and Windows. Version tags create
archives and checksums and use GitHub OIDC plus Cosign for keyless signing.

The OpenAI path currently asks the model for JSON actions instead of depending
on provider-native function-call events. This improves gateway compatibility
and makes strict local output validation essential.
