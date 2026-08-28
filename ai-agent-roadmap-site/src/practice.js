const shared = {
  name: "Your Agent",
  repoPath: "examples/your-agent"
};

const english = {
  ...shared,
  subtitle: "A user-owned, highly customizable Go agent assistant with host-enforced boundaries",
  summary:
    "Your Agent is the executable counterpart of this research map: a runtime users can shape into their own highly customized assistant. It combines a deterministic offline model with an OpenAI-compatible streaming provider, then places both behind the same scheduler-owned execution path, tool policy, persisted Session and Goal state, recoverable planning, lifecycle memory, evaluation, metrics, and user surfaces. Paper research is the included default profile, while the runtime boundaries remain replaceable.",
  stats: [
    { label: "Persistent stores", value: "7", note: "Session, Goal, Plan, Memory, Metrics, Tasks, and Subagents in SQLite" },
    { label: "Built-in tools", value: "15", note: "paper, file, shell, semantic search, web, Skills, and subagents" },
    { label: "Context layers", value: "3", note: "micro-compaction, deterministic digest, asynchronous semantic summary" },
    { label: "User surfaces", value: "5", note: "CLI, readline, TUI, Web/HTTP, and Feishu" }
  ],
  tabs: [
    { id: "timeline", label: "Implementation" },
    { id: "theory", label: "Theory Map" },
    { id: "code", label: "Code Tour" },
    { id: "lessons", label: "Engineering Rules" }
  ],
  phases: [
    {
      order: "01",
      date: "Provider and native tools",
      title: "Make model output observable and replaceable",
      commits: "Provider + Model",
      gitSignal:
        "internal/provider/openai.go owns Responses HTTP/SSE, fallback, retry, prompt-cache, usage, and native function-call transport. internal/model/openai.go parses candidate plans and executes steps through native tool blocks; internal/model/demo.go preserves deterministic offline behavior.",
      built:
        "The same runtime can run with a zero-network DemoModel or a real OpenAI-compatible model. Final answer tokens stream to the UI, native tool calls retain provider call IDs, and API credentials stay in environment variables.",
      theory:
        "Few-shot learning and structured prompting make temporary behavior possible; alignment research improves defaults. Neither makes output a trusted executable contract, so provider text remains an untrusted proposal.",
      practice:
        "Keep transport, model policy, and host execution as separate interfaces. Test SSE fragments, usage, cache fields, transient failures, fallback order, plan JSON, native tool blocks, and gateway compatibility without granting a model direct tool access."
    },
    {
      order: "02",
      date: "Agent and Goal loops",
      title: "Separate local reasoning from long-running continuation",
      commits: "Scheduler + ReAct + Goal",
      gitSignal:
        "internal/agent/agent.go sends each ready plan step through bounded provider-native Tool Calling/ReAct. internal/goal/store.go persists objective, state, tokens, turns, auto-resume, and pause/resume/clear transitions independently from chat messages.",
      built:
        "A Goal can continue across turns and process restarts when evaluation has not accepted the result. Zero token and Goal-turn limits mean unlimited by default, while every scheduled step still has max-steps, cancellation, and explicit stop reasons.",
      theory:
        "Agent-loop research turns a completion into environment feedback. Toward Efficient Agents adds joint outcome and cost measurement. Long-horizon work requires a durable objective rather than repeatedly restating a prompt.",
      practice:
        "Give the scheduler, step ReAct loop, and Goal continuation clear ownership and budgets. Persist Goal transitions, return tool failures as native results, and test cancellation, pause, evaluator retry, unlimited defaults, and explicit budget exhaustion."
    },
    {
      order: "03",
      date: "Tool authority",
      title: "Place real capabilities behind one policy registry",
      commits: "Tools + Plugins + MCP",
      gitSignal:
        "internal/tools registers paper, file, shell, glob/grep, semantic code search, web, clarification, Skills, and durable subagents. internal/plugin loads command plugins and MCP stdio servers through the same registry.",
      built:
        "Every call receives schema validation, read/write/dangerous risk, approval, timeout, cancellation, bounded output, and metrics. File tools reject workspace and symlink escape; web tools reject private and unsafe targets. External tools default to dangerous.",
      theory:
        "Toolformer shows that selection and arguments can be learned. Agent safety and poisoning work show why selection cannot also be authorization. The host must re-evaluate permission at the moment of every side effect.",
      practice:
        "A tool is a capability boundary, not a helper function. Test unknown tools, invalid arguments, denial, approval, timeout, oversized results, path escape, redirect behavior, and process failure before adding more model autonomy."
    },
    {
      order: "04",
      date: "Durable state and context",
      title: "Keep full history while reducing only the model-facing view",
      commits: "Session + Compaction",
      gitSignal:
        "internal/session uses canonical turns/events with native reasoning and tool blocks, whole-turn transactions, legacy migration, status/list/title/fork, L1/L2/L3 compaction, usage accounting, and context-length recovery.",
      built:
        "Restart recovery replays native reasoning state, tool-call IDs, and matching results instead of a text transcript. At a byte or token threshold, the view keeps intact recent tool protocol and a digest; OpenAI mode may add a ready semantic summary.",
      theory:
        "Transformer context cost and HiAgent's hierarchical working memory motivate selective views. Compression is useful when it preserves decisions and evidence while reducing noise; it must not become silent deletion.",
      practice:
        "Commit one user turn atomically, keep the event store as source of truth, and compact only at protocol-safe boundaries. Test restart identity, rollback, legacy migration, stale summaries, async failure, and two-stage provider recovery."
    },
    {
      order: "05",
      date: "Planning and acceptance",
      title: "Turn plans into recoverable execution state",
      commits: "Planner + Scheduler + Verifier",
      gitSignal:
        "internal/planning validates IDs, tools, dependencies, cycles, initial state, and success criteria; plans and evidence live in SQLite; Scheduler runs ready dependencies and Verifier handles output, file, or human checks.",
      built:
        "Model-generated plans survive restart, independent roles can run concurrently, and steps with human checks wait in awaiting_acceptance. CLI and HTTP resume the exact persisted plan through Scheduler.RunReady; interrupted running steps recover without repeating completed work.",
      theory:
        "Planning research distinguishes decomposition from execution monitoring and replanning. Chain-of-thought may suggest steps, but a DAG, statuses, attempts, evidence, and acceptance criteria make them operational.",
      practice:
        "Validate before persistence, dispatch only ready work, record every transition, and make irreversible acceptance explicit. Test dependency cycles, interrupted plans, parallel waves, verifier failure, and human acceptance."
    },
    {
      order: "06",
      date: "Memory and evaluation",
      title: "Store selected experience and verify completion independently",
      commits: "SQLite Memory + Evaluator",
      gitSignal:
        "internal/memory stores scope, source, confidence, active/archived state, and timestamps with count/byte retrieval budgets. internal/evaluator checks required report evidence before Goal completion.",
      built:
        "Stable facts update by (scope, key) rather than growing an append-only JSON file. Obvious credentials are rejected. A final model message is only a candidate until deterministic checks and any plan acceptance succeed.",
      theory:
        "Memory in the LLM Era and A-MEM motivate lifecycle and revision; AgentPoison motivates provenance and distrust. Evaluation benchmarks require environment or artifact evidence instead of model self-reporting.",
      practice:
        "Keep memory separate from Session and trace data, budget retrieval, and never treat recall as permission. Pair every generated artifact with a verifier and retain the evidence that allowed or rejected completion."
    },
    {
      order: "07",
      date: "Interfaces and release",
      title: "Exercise one runtime through product-level surfaces",
      commits: "CLI + TUI + Web + Feishu + Release",
      gitSignal:
        "cmd and internal/ui/input/render/tui/server/integrations expose readline, Markdown, full-screen TUI, persistent task and Session APIs, a file workbench, WebSocket PTY, and Feishu. Make and CI provide E2E, browser, open-source, cross-platform archive, checksum, and signing paths.",
      built:
        "Users can continue the same state through local terminal, browser, or Feishu, including cancellation and approval. Redacted JSONL traces and SQLite metrics make provider, tool, compression, and outcome behavior comparable across runs.",
      theory:
        "Agent engineering is a whole-system property. Trajectory and RL work require structured traces, while efficiency work requires cost at the same boundary as success. A unit-test-only model or scheduler does not prove a usable interface.",
      practice:
        "Reuse one core instead of duplicating semantics in adapters. Test real processes and browser-visible states, preserve security boundaries at every entry point, and release only reproducible artifacts with checksums and documented limits."
    }
  ],
  theoryMaps: [
    { theory: "LLM foundations", implementation: "OpenAI-compatible and deterministic providers", files: ["internal/provider/openai.go", "internal/model/"], explanation: "In-context behavior and alignment live behind provider/model interfaces. Streaming, fallback, cache, usage, and native function calls are transport facts; plans and actions remain host-validated proposals." },
    { theory: "Agent Loop", implementation: "Scheduler-owned native ReAct plus persisted Goal continuation", files: ["internal/agent/agent.go", "internal/planning/", "internal/goal/store.go"], explanation: "The scheduler selects ready steps; each step alternates native tool calls and matching results. The outer Goal continues after failed acceptance, with cancellation, pause, budgets, and stop reasons explicit." },
    { theory: "Memory", implementation: "Scoped lifecycle records and layered context", files: ["internal/memory/store.go", "internal/session/"], explanation: "Selected facts live in budgeted SQLite memory, complete messages remain in Session, and only the model-facing view is compacted." },
    { theory: "Tool Use", implementation: "Policy registry for built-in, plugin, and MCP tools", files: ["internal/tools/", "internal/plugin/"], explanation: "The model selects; the host validates schema, risk, approval, timeout, output, path, and network boundaries before execution." },
    { theory: "Planning", implementation: "Validated persisted DAG with scheduler and verifier", files: ["internal/planning/"], explanation: "Dependencies and acceptance are state, not prose. Ready steps can run concurrently, survive restart, and wait for deterministic or human evidence." },
    { theory: "Evaluation / Safety", implementation: "Independent checks and approval states", files: ["internal/evaluator/", "internal/tools/registry.go"], explanation: "A final answer and a tool request are both candidates. Evaluators, verifiers, and policy determine completion and side effects." },
    { theory: "Efficient Agents", implementation: "Token, cache, latency, tool, compression, and outcome metrics", files: ["internal/metrics/", "internal/trajectory/"], explanation: "The runtime evaluates effectiveness and cost together and preserves enough trace detail to diagnose regressions." },
    { theory: "RL and Skills", implementation: "Replayable versioned trajectory foundation", files: ["internal/trajectory/logger.go", ".agent-data/runs/"], explanation: "The project does not pretend to train a policy. It first records goal, plan, actions, observations, acceptance, cost, and outcome needed for later preference or skill work." }
  ],
  codeTour: [
    { area: "Start at runtime composition", paths: ["cmd/your-agent/main.go", "internal/app/factory.go"], read: "Follow flags and environment into provider, SQLite stores, registry, evaluator, metrics, UI, and lifecycle cleanup." },
    { area: "Trace model transport and parsing", paths: ["internal/provider/openai.go", "internal/model/openai.go"], read: "Separate SSE/retry/fallback/cache handling from plan JSON, native function calls, grouped tool results, and their error paths." },
    { area: "Follow one Goal turn", paths: ["internal/agent/agent.go", "internal/planning/scheduler.go", "internal/goal/store.go"], read: "Observe how ready-step scheduling, native tools, observations, evaluation, budgets, and persisted Goal transitions interact." },
    { area: "Inspect host authority", paths: ["internal/tools/registry.go", "internal/tools/runtime.go", "internal/plugin/plugin.go"], read: "Read schema, risk, approval, timeout, output, path, web, command, and MCP boundaries before looking at happy-path tools." },
    { area: "Separate state owners", paths: ["internal/session/", "internal/planning/", "internal/memory/store.go"], read: "Compare complete conversation, executable plan state, and selected long-term facts; then inspect compaction and recovery." },
    { area: "Read acceptance and observability", paths: ["internal/evaluator/report.go", "internal/metrics/store.go", "internal/trajectory/logger.go"], read: "See what proves completion, what is measured, and which sensitive fields are redacted from replay traces." },
    { area: "Finish at real entry points", paths: ["internal/server/server.go", "internal/integrations/feishu/", "internal/ui/"], read: "Verify that cancellation, approval, Session continuation, and status mean the same thing in terminal, HTTP, Web, and Feishu." }
  ],
  lessons: [
    { title: "A model proposal is not authority", text: "Plan parsing and tool selection are useful only because deterministic code validates structure, permission, budgets, and evidence before changing the environment." },
    { title: "State has multiple owners", text: "Session, Goal, Plan, Memory, Metrics, and Trace serve different recovery and retention needs. A single conversation blob cannot safely replace them." },
    { title: "Compaction is a view", text: "Keep full history and reduce only what the next model call sees. Always provide deterministic fallback and measure summary cost." },
    { title: "Plans must survive interruption", text: "Dependencies, attempts, evidence, and acceptance belong in persisted state so restart does not repeat irreversible work or rely on model recollection." },
    { title: "Evaluation owns stopping", text: "The agent's final text is a candidate. Tests, artifacts, environment state, and human decisions determine whether the Goal is actually achieved." },
    { title: "Product surfaces share one core", text: "CLI, TUI, browser, and Feishu should not implement different permission or continuation semantics. Adapters translate protocols; they do not redefine the agent." },
    { title: "Optimize after observation", text: "Prompt, workflow, skill, or RL changes require a fixed task set and traces that join success, cost, latency, intervention, and safety." }
  ]
};

const chinese = {
  ...shared,
  subtitle: "由用户掌握、可高度定制且由宿主执行边界的 Go Agent 助手",
  summary:
    "Your Agent 是研究图谱的可执行对应物，寓意用户可以把它塑造成自己的高度自定义助手。它把确定性离线模型与 OpenAI-compatible 流式 Provider 放在同一套 Scheduler 主执行链、工具策略、Session、Goal、Planning、Memory、Evaluation、Metrics 和 UI 边界中。论文研究是仓库附带的默认 Profile，Runtime 边界则可以按领域替换。",
  stats: [
    { label: "持久存储", value: "7", note: "Session、Goal、Plan、Memory、Metrics、Tasks、Subagents 均使用 SQLite" },
    { label: "内置工具", value: "15", note: "论文、文件、Shell、语义搜索、网页、Skills 和子 Agent" },
    { label: "上下文层级", value: "3", note: "微压缩、确定性 digest、异步语义摘要" },
    { label: "用户入口", value: "5", note: "CLI、readline、TUI、Web/HTTP、飞书" }
  ],
  tabs: [
    { id: "timeline", label: "实现分层" },
    { id: "theory", label: "理论映射" },
    { id: "code", label: "代码结构" },
    { id: "lessons", label: "工程结论" }
  ],
  phases: [
    {
      order: "01", date: "Provider 与原生工具", title: "让模型输出可观察、可替换", commits: "Provider + Model",
      gitSignal: "internal/provider/openai.go 负责 Responses HTTP/SSE、回退、重试、Prompt Cache、Usage 与原生 function call；internal/model/openai.go 解析计划并用原生 blocks 执行步骤，demo.go 保留确定性离线路径。",
      built: "同一个 Runtime 可连接 DemoModel 或真实 OpenAI-compatible 模型。最终回答可流式进入 UI，原生工具调用保留 Provider call ID，API 凭据只来自环境变量。",
      theory: "Few-shot 与结构化 Prompt 提供临时适配，对齐研究改善默认行为，但都不会把模型输出变成可信执行契约。",
      practice: "把传输、模型策略和宿主执行拆成接口，测试 SSE、Usage、Cache、瞬时错误、回退顺序、计划 JSON、原生工具 blocks 和兼容网关。"
    },
    {
      order: "02", date: "Agent 与 Goal Loop", title: "区分局部推理与长任务 continuation", commits: "Scheduler + ReAct + Goal",
      gitSignal: "internal/agent/agent.go 将每个 ready 步骤交给有界 Provider 原生 Tool Calling/ReAct；internal/goal/store.go 独立保存目标、状态、Token、轮次、自动恢复和 pause/resume/clear。",
      built: "验收未通过时，同一 Goal 可跨轮和重启继续。Token 与 Goal turn 默认零表示不限，每个调度步骤仍有 max-steps、取消和停止原因。",
      theory: "Agent Loop 把完成调用变成环境反馈；高效 Agent 要联合衡量结果和成本；长任务需要持久目标，而不是反复改写 Prompt。",
      practice: "为 Scheduler、步骤 ReAct 与 Goal continuation 定义所有者和预算，持久化 Goal 转移，把工具失败作为原生结果，并测试取消、暂停、重试和预算耗尽。"
    },
    {
      order: "03", date: "工具权限", title: "把真实能力放进统一策略 Registry", commits: "Tools + Plugins + MCP",
      gitSignal: "internal/tools 注册论文、文件、Shell、glob/grep、语义代码搜索、网页、澄清、Skills 和生命周期子 Agent；internal/plugin 通过同一 Registry 加载命令插件和 MCP stdio。",
      built: "每次调用经过 schema、只读/写入/危险等级、审批、超时、取消、输出预算和指标。文件拒绝路径与符号链接逃逸，网页拒绝私网目标，外部工具默认危险。",
      theory: "Toolformer 说明选择和参数可以学习；Agent 安全说明选择不能同时成为授权，每次副作用都要由宿主重新判断。",
      practice: "工具是能力边界。新增自主性前测试未知工具、非法参数、拒绝/审批、超时、大输出、路径逃逸、重定向和进程失败。"
    },
    {
      order: "04", date: "持久状态与上下文", title: "保留原生语义，只压缩送模视图", commits: "Session + Compaction",
      gitSignal: "internal/session 使用 canonical turn/event、原生 reasoning/tool blocks、整轮事务和旧数据迁移，并支持状态/列表/标题/Fork、L1/L2/L3、用量统计和 context-length 恢复。",
      built: "重启后原样续接 reasoning 状态、tool-call ID 与匹配结果，而不是文本回放。达到阈值后只压缩送模视图，并完整保留近期 tool protocol；OpenAI 模式可加入已准备好的语义摘要。",
      theory: "Transformer 的上下文成本与 HiAgent 的层级工作记忆共同说明需要选择性视图；压缩不能等于静默删除。",
      practice: "以用户 turn 为原子事务，把 event store 放在上下文外，在协议安全边界压缩，并测试重启同一性、事务回滚、旧数据迁移、摘要失败和两级恢复。"
    },
    {
      order: "05", date: "Planning 与验收", title: "让计划成为可恢复执行状态", commits: "Planner + Scheduler + Verifier",
      gitSignal: "internal/planning 校验 ID、工具、依赖、环、初始状态和成功条件；计划与证据写入 SQLite；Scheduler 调度 ready 步骤，Verifier 检查输出、文件或人工条件。",
      built: "模型生成计划可跨重启恢复，独立角色可并行；需要人工检查的步骤进入 awaiting_acceptance。CLI 与 HTTP 通过 Scheduler.RunReady 恢复同一计划，中断步骤可恢复且不重复已完成工作。",
      theory: "规划研究区分任务分解、执行监控和重规划。CoT 可以提出步骤，但 DAG、状态、尝试、证据和验收才能执行。",
      practice: "先校验再持久化，只调度 ready 工作，记录所有转移，明确不可逆验收；测试依赖环、中断恢复、并发波次和人工接受。"
    },
    {
      order: "06", date: "Memory 与 Evaluation", title: "保存精选经验，独立验证完成", commits: "SQLite Memory + Evaluator",
      gitSignal: "internal/memory 保存 scope、source、confidence、active/archived 与时间，并限制检索条数/字节；internal/evaluator 在 Goal 完成前检查报告证据。",
      built: "稳定事实按 (scope,key) 更新，不再无限追加 JSON。明显凭据会被拒绝。模型最终消息只有通过确定性检查和计划验收后才完成 Goal。",
      theory: "Memory in the LLM Era 与 A-MEM 强调生命周期；AgentPoison 强调来源和不信任；评估基准要求环境或产物证据。",
      practice: "把 Memory 与 Session/Trace 分开，限制召回，永远不把记忆当权限；每个生成产物配套 Verifier 并保存接受或拒绝证据。"
    },
    {
      order: "07", date: "入口与发布", title: "用产品级入口验证同一 Runtime", commits: "CLI + TUI + Web + Feishu + Release",
      gitSignal: "cmd 与 internal/ui/input/render/tui/server/integrations 提供 readline、Markdown、TUI、持久 Task/Session API、文件工作台、WebSocket PTY 和飞书；Make/CI 提供 E2E、浏览器、开源、跨平台归档、校验和与签名。",
      built: "用户可从终端、浏览器或飞书继续同一状态并取消/审批。脱敏 JSONL 与 SQLite 指标可跨 Run 比较 Provider、工具、压缩和结果。",
      theory: "Agent 工程化是整体属性。轨迹/RL 需要结构化 Trace，高效 Agent 需要在成功边界记录成本。只测单个模型或调度函数不足以证明可用。",
      practice: "所有 Adapter 复用一个核心，测试真实进程和浏览器可见状态，在每个入口保持权限边界，只发布可复现、带校验和并说明局限的产物。"
    }
  ],
  theoryMaps: [
    { theory: "LLM 基础", implementation: "OpenAI-compatible 与确定性 Provider", files: ["internal/provider/openai.go", "internal/model/"], explanation: "流式、回退、Cache、Usage 与原生 function call 属于 Provider；计划和动作仍是不可信提议。" },
    { theory: "Agent Loop", implementation: "Scheduler 主导的原生 ReAct 与持久 Goal continuation", files: ["internal/agent/agent.go", "internal/planning/", "internal/goal/store.go"], explanation: "Scheduler 选择 ready 步骤，每个步骤交替原生工具调用与匹配结果；外层在验收失败后继续，取消、暂停和预算都显式化。" },
    { theory: "Memory", implementation: "分 Scope 生命周期与层级上下文", files: ["internal/memory/store.go", "internal/session/"], explanation: "精选事实进入预算化 SQLite Memory，全量消息留在 Session，只压缩送模视图。" },
    { theory: "Tool Use", implementation: "内置、插件与 MCP 的策略 Registry", files: ["internal/tools/", "internal/plugin/"], explanation: "模型选择，宿主校验 schema、风险、审批、超时、输出、路径和网络边界。" },
    { theory: "Planning", implementation: "带 Scheduler 与 Verifier 的持久 DAG", files: ["internal/planning/"], explanation: "依赖与验收是状态；ready 步骤可并行，跨重启恢复并等待确定性或人工证据。" },
    { theory: "Evaluation / Safety", implementation: "独立检查与审批状态", files: ["internal/evaluator/", "internal/tools/registry.go"], explanation: "最终答案和工具请求都是候选，Evaluator、Verifier 与 Policy 决定完成和副作用。" },
    { theory: "Efficient Agents", implementation: "Token、Cache、延迟、工具、压缩和结果指标", files: ["internal/metrics/", "internal/trajectory/"], explanation: "Runtime 联合比较效果与成本，并保留诊断回归所需的 Trace。" },
    { theory: "RL 与 Skills", implementation: "可回放轨迹基础", files: ["internal/trajectory/logger.go", ".agent-data/runs/"], explanation: "项目先保存目标、计划、动作、观察、验收、成本和结果，不把日志伪装成训练框架。" }
  ],
  codeTour: [
    { area: "从 Runtime 装配开始", paths: ["cmd/your-agent/main.go", "internal/app/factory.go"], read: "沿 Flag 和环境变量查看 Provider、SQLite、Registry、Evaluator、Metrics、UI 和资源关闭。" },
    { area: "追踪模型传输与解析", paths: ["internal/provider/openai.go", "internal/model/openai.go"], read: "区分 SSE/重试/回退/Cache 与计划 JSON、原生 function call、成组 tool result 及错误路径。" },
    { area: "跟随一个 Goal turn", paths: ["internal/agent/agent.go", "internal/planning/scheduler.go", "internal/goal/store.go"], read: "观察 ready-step 调度、原生工具、Observation、Evaluation、预算和 Goal 转移如何互动。" },
    { area: "检查宿主权限", paths: ["internal/tools/registry.go", "internal/tools/runtime.go", "internal/plugin/plugin.go"], read: "先读 schema、风险、审批、超时、输出、路径、网页、命令和 MCP 边界。" },
    { area: "区分状态所有者", paths: ["internal/session/", "internal/planning/", "internal/memory/store.go"], read: "比较完整对话、可执行计划与精选事实，再看压缩和恢复。" },
    { area: "理解验收与可观测性", paths: ["internal/evaluator/report.go", "internal/metrics/store.go", "internal/trajectory/logger.go"], read: "查看什么证明完成、记录哪些指标、哪些敏感字段会脱敏。" },
    { area: "最后看真实入口", paths: ["internal/server/server.go", "internal/integrations/feishu/", "internal/ui/"], read: "确认终端、HTTP、Web 和飞书中的取消、审批、Session continuation 与状态语义一致。" }
  ],
  lessons: [
    { title: "模型提议不是权限", text: "计划解析和工具选择只有经过结构、权限、预算与证据校验后，才能改变真实环境。" },
    { title: "状态有不同所有者", text: "Session、Goal、Plan、Memory、Metrics 和 Trace 服务不同的恢复与保留需求，不能压成一个对话 Blob。" },
    { title: "压缩只是视图", text: "保留全量历史，只减少下一次模型看到的内容，并始终提供确定性 fallback 和摘要成本。" },
    { title: "计划必须能中断恢复", text: "依赖、尝试、证据与验收写入持久状态，重启后不能重复不可逆工作或依赖模型回忆。" },
    { title: "Evaluator 决定停止", text: "最终文本只是候选，测试、产物、环境状态和人工决定 Goal 是否真正 achieved。" },
    { title: "多个产品入口复用同一核心", text: "CLI、TUI、浏览器和飞书不能各自发明权限或 continuation；Adapter 只翻译协议。" },
    { title: "先观测，再优化", text: "Prompt、Workflow、Skill 或 RL 改动都需要固定任务集，以及同时记录成功、成本、延迟、人工介入和安全的轨迹。" }
  ]
};

export const practiceProjects = { en: english, "zh-CN": chinese };
export const practiceProject = chinese;
