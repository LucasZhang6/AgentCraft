# AgentCraft

[English](README.md) | [简体中文](README.zh-CN.md)

AgentCraft connects **agent theory, representative papers, and a runnable
implementation**. About 40% of the project is devoted to paper analysis; the
rest turns those ideas into a Go runtime that can be inspected, tested, and
extended into a personal agent.

The project follows one continuous system model:

> An LLM provides cognitive capacity. The Agent Loop turns cognition into
> action. Memory preserves continuity. Tools connect the model to an
> environment. Planning makes long tasks manageable. Evaluation and safety
> constrain outcomes and permissions. Trajectories support later optimization.
> Engineering turns the whole loop into a product.

![AI Agent capability roadmap from LLM foundations to productization](assets/architecture/agent-capability-roadmap.png)

The repository currently includes:

- 8 agent-system topics and 31 representative paper reviews
- a short overview and a four-part detailed explanation for every paper
- locally cached paper PDFs and an in-browser PDF.js reader
- a production-shaped Paper Agent runtime written in Go
- CLI, TUI, Web UI, HTTP API, and Feishu entry points
- persistent sessions, goals, plans, memory, metrics, and replayable traces
- cross-platform builds, E2E and browser regression, CodeQL, dependency audits,
  complete Trivy scans, SPDX SBOMs, and signed release checks

## Start Here

Requirements:

- Node.js 20 or newer
- Go 1.25 or newer

Download the local paper cache and start the research site:

```bash
git clone git@github.com:LucasZhang6/AgentCraft.git
cd AgentCraft
npm run papers:download
npm run dev
```

Open `http://127.0.0.1:4173`. The site defaults to English and can be switched
to Simplified Chinese from any page. It reuses 25 core PDFs for 31 entries and
downloads one additional Constitutional AI paper for the combined alignment
entry. See the [local paper cache guide](ai-agent-roadmap-site/assets/pdfs/README.md).

Build the site and all Go binaries:

```bash
make build
```

Artifacts are written to `dist/bin/`:

```text
paper-agent
paper-agent-server
feishu-adapter
```

Run the deterministic offline agent:

```bash
npm run agent -- "Explain a representative paper about agent memory"
```

Run the persistent terminal UI:

```bash
dist/bin/paper-agent -interactive -provider demo
```

Run the Web UI and HTTP API:

```bash
dist/bin/paper-agent-server -provider demo
```

Open `http://127.0.0.1:18080/`.

## Research Map

The papers are organized by the dependency structure of an agent system, not
as a fixed course.

| Topic | Central question | Representative work |
| --- | --- | --- |
| LLM foundations | How does the model represent context, adapt, reason, retrieve, and align? | Transformer, GPT-3, CoT, RAG, Toolformer, RLHF, Constitutional AI |
| Agent Loop | How does a model become a stateful decision process? | Autonomous Agent Survey, Rise and Potential, Toward Efficient Agents |
| Memory | What should be retained, retrieved, revised, or forgotten? | Memory in the LLM Era, A-MEM, HiAgent, AgentPoison |
| Tool Use | When should the model act, through which capability, and under whose authority? | Toolformer, AutoGen, MetaGPT, AFlow |
| Planning | How are goals decomposed, scheduled, monitored, and recovered? | Understanding LLM Agent Planning, CoT, multi-agent scaling |
| Evaluation / Safety | Did the environment actually change as intended, without crossing a boundary? | SPA-Bench, CyBench, AgentPoison |
| RL optimization | How can preferences and trajectories improve policy? | DPO, DeepSeek-R1, Search-R1, AgentGym-RL, SkillRL |
| Engineering | How do the modules become a recoverable and observable product? | AutoGen, MetaGPT, AFlow, Toward Efficient Agents |

Each site entry has two layers:

- **Overview**: the research question, core method, and main contribution.
- **Detailed explanation**: intuition, method and evidence, agent-system
  relevance, and implementation limits.

The local PDF remains the source of truth for equations, experimental setup,
citations, and conclusion boundaries. The project analysis is an engineering
interpretation, not a replacement for the paper.

## What the Papers Change in a System

The reading map is most useful when every idea is tied to a runtime object:

| Theory | Runtime object | Evidence to retain |
| --- | --- | --- |
| In-context learning | prompt assembly and provider routing | model, prompt version, input tokens, latency |
| Agent Loop / ReAct | controller state machine | decision, action, observation, stop reason |
| Toolformer | tool registry and execution policy | schema result, permission, duration, bounded output |
| Planning research | validated DAG and scheduler | dependencies, attempts, evidence, acceptance state |
| Memory research | scoped memory lifecycle | source, confidence, status, timestamps, retrieval budget |
| HiAgent | layered working context | recent turns, deterministic digest, semantic summary |
| Evaluation benchmarks | verifier and acceptance gates | checks, external evidence, human decision |
| DPO / Agent RL | trajectory and metric stores | complete trace, reward or preference, cost, outcome |

This mapping keeps the project honest. A model-generated plan is not yet a
recoverable plan; a vector database is not yet a memory lifecycle; a fluent
answer is not yet task completion.

## Current Paper Agent Runtime

`examples/paper-agent/` is a Go implementation of the system described above.
It retains a deterministic `DemoModel` for offline regression and supports an
OpenAI-compatible Responses provider for real model execution.

![Paper Agent control loop with planning, tools, evaluation, and memory](assets/architecture/minimal-agent-loop.png)

### Runtime Layers

| Layer | Current implementation |
| --- | --- |
| Provider | OpenAI-compatible Responses API, SSE streaming, bounded retry, model fallback, prompt-cache accounting, usage metrics, custom base URL |
| Controller | ReAct-style `Decide -> Tool -> Observation`, cancellation, explicit stop reasons, context recovery |
| Goal | independent SQLite lifecycle with pause, resume, clear, auto-resume, accumulated tokens and turns; zero limits mean unlimited by default |
| Planning | model-generated structured plans, deterministic validation, SQLite persistence, dependency scheduling, role-based parallel steps, verifier, human acceptance |
| Tools | paper lookup, workspace file operations, shell, grep/glob, web fetch/search, clarification, subagent, command plugins, and MCP stdio tools |
| Tool policy | allowlist, JSON-like schema validation, read/write/dangerous risk levels, approval, timeout, bounded output, workspace and network boundaries |
| Memory | SQLite records with scope, source, confidence, active/archived status, timestamps, upsert semantics, result-count and byte retrieval budgets |
| Session | structured messages and events in SQLite, status and usage, titles, list and fork support, full history retained across restarts |
| Context | L1 micro-compaction, L2 deterministic digest, asynchronous L3 model summary, and two-stage context-length recovery |
| Evaluation | deterministic report checks plus plan-step evidence and optional human acceptance |
| Observability | redacted JSONL trajectories, run metrics, cumulative success rate, tool and approval metrics, session-summary usage |
| Interfaces | one-shot CLI, readline history and search, Markdown terminal rendering, full-screen TUI, Web UI, async HTTP API, Feishu sidecar |
| Delivery | `make build`, E2E process tests, Playwright desktop/mobile regression, open-source checks, cross-platform CI, archives, checksums, and optional Cosign signing |

### Two Nested Loops

Paper Agent separates a local reasoning loop from long-running goal
continuation:

1. A **Goal turn** runs a bounded ReAct loop. The model may call a tool, read
   the observation, revise its decision, and eventually propose a final result.
2. The **Goal loop** evaluates that result. If acceptance fails, it compresses
   old observations and continues the same persisted goal in another turn.

`goal-turns=0` and `token-budget=0` mean no local limit. Individual turns are
still bounded by `max-steps`; cancellation, pause, evaluator success, explicit
budgets, and unrecoverable failures remain stop conditions.

### State Is Deliberately Split

The runtime does not store everything in one conversation blob:

```text
sessions.db   complete messages, structured events, summary usage
goals.db      goal identity, state, continuation counters, token usage
plans.db      DAG steps, dependencies, attempts, evidence, acceptance
memory.db     selected cross-task facts with lifecycle and provenance
metrics.db    per-run outcome and cumulative efficiency metrics
runs/*.jsonl  replayable and redacted execution trajectories
```

This separation follows the theory: conversation history, task state, an
execution plan, long-term memory, and an audit trace have different owners and
retention rules.

### Session Compaction

Full messages remain in SQLite. Compaction only changes the view sent to the
model:

1. **L1 micro-compaction** removes repeated or oversized detail from older
   messages.
2. **L2 deterministic digest** preserves goals, conclusions, failures, and URLs
   without requiring another model call.
3. **L3 semantic summary** is prewarmed asynchronously at 75% of the threshold
   in OpenAI mode. It has a hard timeout and falls back to L2 without blocking
   the user request.

If the provider still reports a context-length error, the runtime retries with
one recent turn and then with only the current turn. Summary calls, tokens, and
latency are recorded separately so compression cost does not disappear from
the metrics.

### Model Proposes, Host Authorizes

The model emits a structured action. The host validates the decision type,
tool name, arguments, risk, approval, timeout, output budget, and remaining
task budget before execution. File tools reject workspace escape and symbolic
link bypass; web tools reject loopback, private, and link-local targets and
unsafe redirects. External plugins and MCP tools default to `dangerous`.

This is the practical consequence of Toolformer, AgentPoison, and agent-safety
research: learned tool selection is a policy signal, never an authorization
mechanism.

## Use a Real Provider

Credentials are read only from environment variables:

```bash
export OPENAI_API_KEY="..."
export OPENAI_BASE_URL="https://your-compatible-provider.example/v1"
export OPENAI_MODEL="your-model-id"

dist/bin/paper-agent \
  -provider openai \
  -fallback-models "fallback-a,fallback-b" \
  "Compare A-MEM and HiAgent, then propose a memory schema"
```

The model creates candidate plans and actions. The Go host still owns DAG
validation, tool schemas, permissions, timeouts, evaluation, and cost limits.
See the full [Paper Agent runtime guide](examples/paper-agent/README.md).

## Verification

Run content, site, and Go tests:

```bash
npm test
npm run papers:verify
```

Run product-level checks:

```bash
make e2e
make browser-regression
make open-source-check
make release-snapshot
make verify-release
```

These checks cover deterministic agent runs, provider streaming/fallback/cache
parsing, tool policy, SQLite stores, goal continuation, plan recovery,
compaction fallbacks, HTTP task state, Feishu events and approval, terminal UI,
browser behavior, release archives, and checksums.

## Repository Layout

```text
.
├── ai-agent-roadmap-site/       # bilingual research map and local PDF reader
├── examples/paper-agent/        # Go runtime, CLI, HTTP server, and adapters
├── docs/                        # architecture, engineering, roadmap, and corpus
├── assets/architecture/         # ImageGen architecture diagrams
├── agent_research_map_from_feishu_urls.md
└── feishu_agent_urls.md
```

Key documents:

- [Architecture](docs/architecture.md)
- [Engineering implementation](docs/engineering-practice.md)
- [Paper Agent runtime](examples/paper-agent/README.md)
- [Feishu adapter](docs/feishu-adapter.md)
- [Research roadmap](docs/roadmap.md)
- [Paper review format](docs/paper-reading-template.md)
- [Documentation languages](docs/i18n.md)
- [Architecture image sources](assets/architecture/README.md)

## Design Principles

- Build a verifiable loop before adding more autonomy.
- Optimize effectiveness and cost together.
- Let the model propose actions; let deterministic code authorize them.
- Keep full history, working context, task state, and long-term memory separate.
- Treat plans as recoverable state, not display text.
- Prefer environment evidence over model self-reporting.
- Preserve trajectories before attempting skill extraction or RL.
- Add multiple agents only when parallelism, isolation, or specialization earns
  the coordination cost.
- Keep English as the canonical documentation language and maintain reciprocal
  Simplified Chinese documents.

## Scope and Next Steps

The current runtime is a substantial reference implementation, not a claim of
production certification. Notable next steps include stronger PDF extraction
and citation verification, richer memory conflict handling, encrypted Feishu
callbacks and long connections, reproducible real-task evaluation sets, and
versioned skills derived from validated trajectories.

## Contributing

Contributions are welcome for paper analysis, factual corrections,
reproducible experiments, runtime capabilities, evaluation tasks, failure
cases, accessibility, and translations. Read [CONTRIBUTING.md](CONTRIBUTING.md)
before opening a pull request. Report security issues privately according to
[SECURITY.md](SECURITY.md).

## License

Code and original writing are licensed under the [MIT License](LICENSE).
Mozilla PDF.js is distributed under Apache-2.0; see its
[license](ai-agent-roadmap-site/assets/vendor/pdfjs/LICENSE). External papers,
projects, and concepts remain the property of their respective authors. Local
PDFs are user-downloaded caches and are not distributed with this repository.
