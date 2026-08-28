# Your Agent Runtime

[English](README.md) | [简体中文](README.zh-CN.md)

Your Agent is a user-owned, highly customizable Go agent assistant whose
engineering boundaries are derived from the research covered by this
repository. It supports deterministic offline regression and real
OpenAI-compatible execution, while keeping providers, prompts, plans, tools,
permissions, sessions, goals, memory, evaluation, adapters, and observability
under host control.

The included paper-research behavior is the default profile and reference test
case, not a hard product boundary. Replace the profile-specific prompts, paper
tools, memory scopes, and evaluator to adapt the same runtime to another domain.

![Your Agent goal, planning, tool, observation, evaluation, and memory loop](../../assets/architecture/minimal-agent-loop.png)

## Build and Run

From the repository root:

```bash
make build
```

The binaries are placed in `dist/bin/`.

Run the persistent readline CLI:

```bash
dist/bin/your-agent -interactive -provider demo
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
dist/bin/your-agent -tui -provider demo
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

dist/bin/your-agent \
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
- interruption recovery before output, plus complete-block salvage without replay
- bounded backoff for 429 and 5xx responses
- an ordered primary/fallback model chain
- `prompt_cache_key` and cache read/create token accounting
- request deadlines and response-size limits
- input, output, total, and cached token usage
- `store=false`, because the local runtime owns state

The final report can stream to the UI, while internal scheduled-step output is
kept separate.
If an upstream stream breaks before semantic output, the provider retries the
same request within its retry budget. If Responses API has emitted complete
`output_item.done` blocks, the runtime keeps only those valid blocks, marks the
turn truncated, executes complete tool calls at most once, and persists a
continuation message telling the model not to repeat successful work. An
unfinished delta or malformed partial call is never guessed or replayed.
The official OpenAI endpoint receives `max_output_tokens`. Custom gateways omit
that optional field for compatibility; configure an output limit on the gateway
as well as relying on the local response-body bound.

`internal/model/openai.go` asks the provider for a structured plan, then uses
native function calls for step execution. Plans must parse and pass DAG
validation; native tool calls still pass local schema, permission, and
evaluation gates.

## HTTP and Web UI

Start the asynchronous API server:

```bash
dist/bin/your-agent-server -provider openai
```

Open `http://127.0.0.1:18080/`. Task states include `queued`, `running`,
`pending_approval`, `completed`, `failed`, `canceled`, and `interrupted`.
Reusing a `session_id` continues the same conversation.

Important endpoints include:

```text
/api/agent/execute
/api/agent/status
/api/agent/events
/api/agent/cancel
/api/agent/approve
/api/session/status
/api/sessions
/api/sessions/messages
/api/sessions/events
/api/sessions/fork
/api/tasks
/api/files
/api/files/content
/api/files/download
/api/terminal/ws
/api/skills
/api/subagents
/api/goal/status
/api/goal/action
/api/plan/latest
/api/plan/accept
```

Set `YOUR_AGENT_ACCESS_ID` to require a login exchange for a Bearer token. The
embedded workbench manages Sessions, files, tasks, Skills, subagents, plans,
and a real PTY terminal. Tasks persist in `.agent-data/tasks.db`; unfinished
work becomes `interrupted` after restart instead of appearing active forever.
The server uses a bounded worker queue: different Sessions may run concurrently,
while tasks for one Session are serialized to preserve canonical turn order.
Defaults are 4 active tasks, 256 queued tasks, and 8 queued-or-active tasks per
Session. Configure them with `--max-concurrent-tasks`, `--max-queued-tasks`, and
`--max-session-tasks`, or their `YOUR_AGENT_MAX_*` environment variables.

SSE events carry monotonically increasing IDs. A client can reconnect with
`Last-Event-ID` or `/api/agent/events?task_id=...&after=N`; the embedded Web UI
does this automatically, so a dropped browser connection resumes from the last
observed task message instead of starting the Agent again.
The default loopback-only configuration is intended for local use, not direct
public exposure.

## Feishu Adapter

`feishu-adapter` is a separate sidecar over the HTTP API. It supports text,
rich-text, quoted messages, images, explicit group mentions, event deduplication,
durable chat-to-session mapping, polling, cancellation, and approval cards.

The sidecar owns Feishu credentials; the Agent Server owns the LLM key and all
runtime permissions. See the [Feishu adapter guide](../../docs/feishu-adapter.md).
Your Agent does not copy the scheduler notifier from the reference Verdent
implementation because it has no scheduled-task subsystem.

## Scheduler, Agent, and Goal Loops

Execution has three cooperating levels:

1. `Scheduler.RunReady` dispatches only dependency-ready persisted steps and
   can run independent roles concurrently.
2. Each step runs provider-native `assistant tool_call -> host execution ->
   tool_result -> model` for at most `max-steps` model turns. A step declares an
   `allowedTools` set, and every native call must remain inside that set.
   Explicitly parallel-capable read tools may overlap in concurrency lanes;
   results are returned in original provider-call order. Writes and dangerous
   tools remain serial.
3. If the evaluator rejects the completed result, the persisted Goal compresses
   old observations and starts another scheduler wave.

`goal-turns=0` and `token-budget=0` mean unlimited. Pause, cancellation,
evaluator success, explicit limits, and unrecoverable failures can still stop
the Goal.

The model-generated plan must pass deterministic validation before execution:
unique step IDs, existing dependencies, an acyclic graph, a non-empty controlled
set of available tools, non-empty success criteria, and valid initial state.
The legacy singular `tool` field is normalized into `allowedTools`; model output
does not grant execution authority.

## Tool Catalog and Policy

Built-in tools include:

```text
search_papers        read_paper_card
file_read            file_write            file_edit
list_dir             glob                  grep
semantic_code_search bash                  web_fetch
web_search           clarification         skill
subagent
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

Parallel execution is opt-in on each tool definition and is honored only for
`read` risk. Calls in the same concurrency lane are serialized, while distinct
read lanes may overlap. This prevents model output order from becoming a race:
the grouped `tool_results` message always follows the original call order.

Large string results are truncated; oversized structured results are rejected
instead of being silently corrupted. File tools reject workspace escape and
symbolic-link bypass. Web tools reject private, loopback, and link-local targets
and unsafe redirects.

`semantic_code_search` builds a bounded local symbol/lexical index and supports
`search`, `definition`, and `references`. Skills are Markdown documents with
YAML frontmatter loaded from `skills/`, `.your-agent/skills/`, or the user
directory, with dependency and prompt-size checks.

The `subagent` tool is a lifecycle controller rather than a one-shot helper. It
supports `spawn`, `status`, `wait`, `cancel`, and `list`; each child is persisted
in `.agent-data/subagents.db` with parent Session/Run, timeout, result, and error.
Every child owns an independent provider-native message history and bounded
ReAct loop. It receives the runtime registry and policy, can select among its
authorized tools, and records native call/result pairs. Recursive `subagent`
access is excluded by default; child cancellation and timeout are explicit
lifecycle authorities rather than artifacts of the short parent tool call.

### Plugins and MCP

`.your-agent/plugins.json` can register command plugins or MCP stdio servers:

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

Complete native conversation state lives in `.agent-data/sessions.db`. Each
user turn buffers `user`, `assistant_blocks`, `tool_results`, and final
`assistant` messages together with runtime events, terminal status, and metrics,
then commits every projection in one SQLite transaction. Blocks retain images,
reasoning IDs and encrypted/raw provider state, tool arguments, tool-call IDs,
results, and errors. Restart recovery sends those native items back to the
provider; the rendered transcript is not used as the continuation protocol.

`session_turn_events` is the ordered source of truth. `session_messages` and
`session_turn_metrics` are query projections used by history and status. The
store removes orphaned tool-call/result pairs from the model view, migrates
legacy text messages into canonical completed turns, and reconstructs Fork
history under new turn IDs. Failed, cancelled, and timed-out turns stay in the
canonical audit log but are excluded from resumable history unless explicitly
retained after protocol validation. Sessions can be listed, inspected, titled, and
forked. Compaction never deletes stored blocks; it only changes the next
model-facing view.

Default compaction triggers are a rendered context at 400 KB or a prior input
above 120K tokens:

- L0 bounds old tool results above 16 KB to a 4 KB head and tail, while leaving
  the newest tool result intact; oversized old text and repeated text are also
  reduced
- L1 replaces stale results from repeated identical tool/argument calls with a
  compact superseded marker, preserving every call ID and result pairing
- L2 cuts only at a plain user text/image boundary, keeps the most recent four
  user turns by default, and adds a deterministic digest of older evidence
- L3 asynchronously prewarms a semantic summary at 75% of the threshold in
  OpenAI mode and uses it only when it covers the whole L2 prefix
- L3 has a 12-second hard timeout and falls back to L2 without blocking the
  current request
- the same L0/L1 housekeeping runs before every model call inside a ReAct step;
  a context-length error shrinks that in-flight native history to one recent
  turn and then zero recent turns without restarting completed tools

Per-turn state and cumulative token, duration, tool-call, tool-failure, and
success metrics are shown by `/session` or `/api/session/status`. Summary calls,
input/output tokens, and latency are stored separately on the Session row. Set
`YOUR_AGENT_SESSION_LLM_SUMMARY=0` to disable L3 for offline, quota-limited, or
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
each scheduled step remains bounded by `max-steps`.

## Persistent Planning

Plans live in `.agent-data/plans.db`. Steps retain dependencies, role, status,
attempts, evidence, acceptance checks, and a controlled `allowedTools` set. The
scheduler only dispatches ready steps and can run independent
researcher/reviewer roles concurrently. Within one step the model can combine
several authorized tools, but it cannot reach tools outside that set.

Deterministic verifiers inspect file and output evidence. A `human` check moves
the step to `awaiting_acceptance`, where CLI or HTTP must explicitly accept it.
This makes planning execution state, not decorative text.

A host-owned Verification Gate observes successful file mutations and
verification commands. When material work has no observed test/build/lint
evidence, it appends a persisted verification step and refuses false completion.

`/plan resume <id>` restores that exact DAG. Interrupted `running` steps return
to `pending`, completed steps retain their output, and `Scheduler.RunReady`
continues the dependency graph without re-planning or text replay. The HTTP task
API exposes the same behavior through `resume_plan_id`.

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
| native `tool_call -> tool_result` inside a scheduled step | ReAct-style feedback between reasoning and environment |
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
| `cmd/your-agent/main.go` | CLI, readline/TUI mode, provider flags, approval, and metrics |
| `cmd/your-agent-server/main.go` | bounded HTTP queue, durable tasks, resumable SSE, Session API, file workbench, WebSocket PTY, cancellation, and approval |
| `cmd/feishu-adapter/main.go` | Feishu callback sidecar |
| `internal/agent/agent.go` | ReAct, Goal continuation, observation compaction, stop conditions |
| `internal/provider/openai.go` | OpenAI-compatible Responses transport |
| `internal/model/` | deterministic and real planner/native-step models, session summarizer |
| `internal/tools/` | registry, paper, file, shell, semantic search, web, clarification, Skill, and subagent tools |
| `internal/skills/` | repository/project/user Markdown Skills and dependency checks |
| `internal/subagent/` | durable child lifecycle, parentage, and independent tool-enabled runtime |
| `internal/verification/` | material-work verification gate |
| `internal/plugin/` | command plugins and MCP stdio integration |
| `internal/session/` | messages/events, L0/L1/L2/L3 views, recovery, status and fork |
| `internal/goal/` | independent Goal lifecycle |
| `internal/planning/` | validation, SQLite plan store, scheduler, verifier and acceptance |
| `internal/memory/` | SQLite memory lifecycle and budgeted retrieval |
| `internal/metrics/` | run metrics and cumulative task success |
| `internal/evaluator/` | deterministic report acceptance |
| `internal/trajectory/` | concurrent redacted JSONL trace |
| `internal/server/` | bounded task scheduling, persistent state, resumable SSE, Session/files/subagent APIs, Web UI, and PTY protocol |
| `internal/input/`, `render/`, `tui/`, `ui/` | terminal input and presentation |
| `internal/app/factory.go` | runtime composition and resource lifecycle |

## Key Flags

```text
-provider demo|openai
-model <model-id>
-fallback-models <id,id>
-base-url <openai-compatible-base-url>
-work-dir <workspace-root>
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

The runtime requires Go 1.25 or newer. SQLite uses the pure-Go
`modernc.org/sqlite` driver, so Windows builds do not require CGO or a C
compiler.

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
