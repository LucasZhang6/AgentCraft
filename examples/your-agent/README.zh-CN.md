# Your Agent Runtime

[English](README.md) | [简体中文](README.zh-CN.md)

Your Agent 是一个由用户掌握、可高度定制的 Go Agent 助手，也是本仓库把论文理论映射到工程边界的参考 Runtime。它保留可离线回归的 `DemoModel`，同时支持 OpenAI-compatible Responses API 流式输出与模型回退、Prompt Cache 指标、受控文件和 Shell 工具、本地语义代码搜索、网页、澄清、Skills、生命周期子 Agent、插件与 MCP、结构化 Session、独立 Goal、可恢复 Planning、Verification Gate、SQLite Memory、四层上下文压缩、readline CLI、TUI、Web 工作台和飞书入口。

仓库内置的论文研究能力是默认 Profile 和回归样例，不是产品用途的硬边界。替换领域 Prompt、论文工具、Memory scope 和 Evaluator 后，可以沿用同一套 Runtime 构建个人知识、开发、运维或其他专属 Agent。

![Minimal Agent Loop 从目标、计划和工具行动到评估、记忆与停止的运行闭环](../../assets/architecture/minimal-agent-loop.png)

## 运行方式

在仓库根目录构建站点和三个 Go 程序：

```bash
make build
```

产物位于 `dist/bin/`。启动持久交互终端：

```bash
dist/bin/your-agent -interactive -provider demo
```

交互终端使用 readline，支持持久历史、`Ctrl+R` 搜索和补全。命令包括 `/session`、`/memory`、`/model`、`/goal pause|resume|clear|status`、`/plan show|list|resume|accept`、`/todo`、`/paste`、`/clear` 和 `/exit`。运行中可以用 `Ctrl+C` 取消当前任务而不丢失 Session。

全屏 TUI：

```bash
dist/bin/your-agent -tui -provider demo
```

在仓库根目录运行离线模式：

```bash
npm run agent -- "解读一篇关于 Agent Memory 的论文"
```

也可以在当前目录运行：

```bash
go run ./cmd/your-agent -provider demo "解读 Tool Use 的代表性论文"
```

真实模型模式只从环境变量读取 API Key：

```bash
export OPENAI_API_KEY="..."
export OPENAI_MODEL="your-model-id"

go run ./cmd/your-agent \
  -provider openai \
  -fallback-models "fallback-a,fallback-b" \
  "解读 Agent Planning 的代表性论文"
```

兼容 OpenAI Responses API 的网关可以通过 `OPENAI_BASE_URL` 或 `-base-url` 接入。模型 ID 不在代码中写死，避免把某个时间点的模型列表固化进项目。

## HTTP 与飞书入口

`your-agent-server` 提供 Verdent 风格的异步任务协议：

```bash
dist/bin/your-agent-server -provider openai
```

浏览器打开 `http://127.0.0.1:18080/` 即可使用内嵌 Web 工作台。除 `/api/agent/*` SSE/审批/取消外，服务还提供 `/api/sessions*`、`/api/tasks`、`/api/files*`、`/api/terminal/ws`、`/api/skills`、`/api/subagents`、`/api/goal/*` 和 `/api/plan/*`。异步任务持久化到 `tasks.db`，具有 `queued`、`running`、`pending_approval`、`completed`、`failed`、`canceled`、`interrupted` 状态；相同 `session_id` 会继续既有结构化对话。

服务端使用有界 Worker Queue：不同 Session 可以并发，同一 Session 严格串行，以维持 canonical turn 顺序。默认最多同时运行 4 个任务、等待队列 256 个任务、每个 Session 最多 8 个等待或运行中的任务；可通过 `--max-concurrent-tasks`、`--max-queued-tasks`、`--max-session-tasks` 或对应的 `YOUR_AGENT_MAX_*` 环境变量调整。SSE 事件携带单调递增 ID，客户端可使用 `Last-Event-ID` 或 `?after=N` 从上次消息继续；内嵌 Web UI 会自动重连，不会因为浏览器流中断而重新执行 Agent。进程重启不会重放未完成任务，而是把它们明确标为 `interrupted`。

飞书适配器复用这组 API，支持消息、引用、富文本、图片、群聊显式 @、事件去重、任务轮询、取消和审批卡片。配置方法见 [飞书适配器](../../docs/feishu-adapter.zh-CN.md)。LLM API Key 只属于 Agent Server，飞书 sidecar 不读取它。Verdent 的 scheduler notifier 没有复制，因为 Your Agent 当前没有定时任务子系统。

## Scheduler、Agent 与 Goal Loop

一次运行包含三个协作层次：

1. `Scheduler.RunReady` 只调度持久计划中依赖已完成的步骤，并可并发执行独立角色。
2. 每个步骤内部运行 Provider 原生 `assistant tool_call -> 宿主执行 -> tool_result -> 模型` ReAct，默认最多 6 个模型 turn；步骤声明 `allowedTools` 受控集合，每次原生调用都必须属于该集合。显式声明支持并行的只读工具可以按 concurrency lane 重叠执行，但结果仍按 Provider 原调用顺序回送；写入和危险工具始终串行。
3. 如果报告尚未通过 Evaluator，外层 Goal continuation 会压缩旧 observation 并进入下一轮 Scheduler wave。`goal-turns=0` 和 `token-budget=0` 默认均表示不限；仍可通过取消、暂停、Evaluator 或显式预算停止。

模型生成的计划在执行前必须通过程序校验：步骤 ID 唯一、依赖存在、依赖图无环、`allowedTools` 中每个工具均存在且获准、成功条件非空。旧的单数 `tool` 字段会被归一化到工具集合；模型输出不直接获得执行权限。

## OpenAI Provider

`internal/provider/openai.go` 使用 Responses API：

- Bearer Token 认证，API Key 不写入配置和日志
- 支持自定义 Base URL
- SSE 增量输出；动作走原生 Tool Calling，最终报告流不混入内部步骤输出
- 上游流在尚未输出语义前中断时有限重试；若已经收到完整 `output_item.done` block，只保留这些可校验的完整块并把 turn 标记为 truncated，完整工具调用至多执行一次，再持久化“不重复已成功调用”的 continuation 消息；残缺 delta 或半截参数不会被猜测或重放
- 429 和 5xx 有限重试、退避与模型 fallback chain
- `prompt_cache_key`，记录 cache read/create token
- 单请求超时和响应大小限制
- 读取 `input_tokens`、`output_tokens` 和 `total_tokens`
- `store=false`，完整 Agent 状态由本地 runtime 管理

官方 OpenAI 地址会发送 `max_output_tokens`。自定义 OpenAI-compatible Base URL 按 Verdent 的兼容策略省略该可选字段，因为不少企业网关会直接拒绝它；这类网关应在服务端配置输出上限，Your Agent 仍保留响应体字节限制。

`internal/model/openai.go` 仍以结构化 JSON 生成计划，随后由程序校验 DAG；步骤执行改用 Provider 原生 Tool Calling/ReAct，assistant tool-call 与成组 tool-result blocks 通过 call ID 配对。工具 schema、权限、超时和 Evaluator 仍由宿主负责。

## 工具安全

工具注册项包含 `read`、`write`、`dangerous` 风险等级。`write` 和 `dangerous` 默认需要审批；非交互环境的 `ask` 模式会拒绝执行。每个工具还受到以下边界约束：

- allowlist 与参数 schema
- 单工具超时
- 序列化输出字节预算
- 结构化大结果拒绝，字符串大结果截断
- 工具失败作为 observation 返回给模型
- 审批次数、耗时和失败次数进入运行指标

并行能力由每个工具显式选择，而且只对 `read` 风险生效；同一 concurrency lane 内仍串行，不同只读 lane 可以重叠。无论完成先后，成组 `tool_results` 始终按原始 call 顺序排列，避免并发改变 Provider 协议。

内置工具包括 `file_read`、`file_write`、`file_edit`、`list_dir`、`glob`、`grep`、`bash`、`web_fetch`、`web_search`、`clarification`、`subagent`、`search_papers` 和 `read_paper_card`。文件工具拒绝工作区逃逸和符号链接绕过；网页工具拒绝私网、回环、链路本地地址和不安全重定向。

`subagent` 是持久化生命周期控制器，支持 `spawn`、`status`、`wait`、`cancel` 和 `list`。每个子 Agent 拥有独立的 Provider 原生消息历史与有界 ReAct 循环，继承 Runtime 的 Registry、权限与审批策略，可在授权工具集合中自行组合调用，并持久化原生 call/result 配对。默认不向子 Runtime 暴露 `subagent`，避免无界递归；子任务超时与取消独立于发起它的短生命周期工具调用。

项目可在 `.your-agent/plugins.json` 注册命令插件或 MCP stdio server。外部工具会获得 `plugin__...` 名称并默认标记为 `dangerous`，仍须通过 schema、超时、输出预算和人工审批：

```json
{
  "version": 1,
  "plugins": [{
    "name": "lookup",
    "command": ["./bin/lookup"],
    "input_schema": {"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}
  }],
  "mcp_servers": [{"name":"workspace","command":["npx","-y","@example/mcp-server"]}]
}
```

## Goal 与持久 Planning

Goal 独立保存在 `.agent-data/goals.db`，按 Session 绑定但不混入消息表。它支持 `pause`、`resume`、`clear`、`auto_resume`、累计 token、轮次和状态历史；进程重启后继续的是同一个目标，而不是重新包装一条 Prompt。

模型生成的 DAG 保存在 `.agent-data/plans.db`。每个步骤保存依赖、角色、状态、尝试次数、证据、acceptance checks 和受控 `allowedTools` 集合。Scheduler 只调度依赖已完成的步骤，可并行运行 researcher/reviewer 等独立角色；一个步骤可以组合使用多个已授权工具，但不能越过该步骤的集合。文件和输出检查由确定性 Verifier 执行，`human` 检查进入 `awaiting_acceptance`，由 CLI 或 HTTP 显式验收。

`/plan resume <id>` 恢复同一个 DAG：中断遗留的 `running` 步骤回到 `pending`，已完成步骤保留输出，再由 `Scheduler.RunReady` 继续依赖调度；不会重新规划或把旧计划变成文本回放。HTTP 任务 API 通过 `resume_plan_id` 提供同样能力。

## SQLite Memory

长期记忆保存在 `.agent-data/memory.db`。每条记录包含：

- `scope`：当前支持任意命名空间，运行时默认检索 `user`、`project`、`learning-preference`
- `source` 和 `confidence`
- `active` / `archived` 生命周期状态
- 创建、更新和最近使用时间
- `(scope, key)` 唯一约束，用于更新已有记忆

检索会同时应用条数预算和字节预算，再按 scope、关键词、置信度和更新时间排序。明显包含 API Key、Token 或密码的内容会被拒绝写入。

## Session Compaction

Session 的完整原生对话状态保存在 `.agent-data/sessions.db`。每个用户 turn 会先缓冲 `user`、`assistant_blocks`、`tool_results`、最终 `assistant`，以及 Runtime 事件、终态和指标，再用一个 SQLite 事务提交全部投影。Block 保留图片、reasoning ID 与 encrypted/raw Provider 状态、工具参数、tool-call ID、结果和错误；重启后这些原生 items 会直接交回 Provider，渲染后的 transcript 不参与 continuation 协议。

`session_turn_events` 是有序事实源，`session_messages` 和 `session_turn_metrics` 是历史与状态查询投影。送模前会移除失去配对的 tool call/result；旧文本消息会迁移成 canonical completed turn，Fork 历史也会用新的 turn ID 重建。失败、取消和超时 turn 完整留在 canonical 审计事件中，但默认不进入可续接历史；只有调用方确认工具协议完整后才显式保留。压缩只改变下一次送给模型的视图，不删除数据库 blocks。默认在渲染上下文达到 400 KB，或上一轮输入达到 120K tokens 时触发：

- L0：旧 tool result 超过 16 KB 时只保留 4 KB 头尾，最新 tool result 完整保留；同时限制超大旧文本与重复文本
- L1：相同工具和参数再次调用后，把更早的 stale result 换成 superseded 标记，但保留 call ID 与 call/result 配对
- L2：只在普通 user text/image 边界裁剪，默认保留最近 4 个用户轮次，并用确定性 digest 保存更早证据
- L3：OpenAI 模式在压缩阈值 75% 时后台预生成语义摘要，单次硬超时 12 秒；仅使用已就绪且覆盖整个 L2 丢弃前缀的摘要
- L3 失败、超时或尚未就绪时自动退回 L2，不阻塞当前用户请求
- 每次 ReAct 模型调用前都会执行同一套 L0/L1 视图整理；Provider 返回 context-length 错误时，在当前步骤内依次缩到最近 1 轮和 0 个历史轮次，不重新执行已经完成的工具

CLI、HTTP Server 和飞书使用同一语义。每轮状态，以及累计 Token、耗时、工具调用、工具失败和成功指标可由 `/session` 或 `/api/session/status` 查看；摘要调用数、输入/输出 Token 与耗时独立记账。这里解决的是跨对话轮次的 Session 窗口；Agent Loop 内部还有一层 observation 压缩。

L3 在 `provider=openai` 时默认开启；离线、配额受限或故障注入测试可设置 `YOUR_AGENT_SESSION_LLM_SUMMARY=0` 显式关闭。

## Agent Context Compaction

运行时把上下文拆成三层：

- `recentObservations`：保留工具调用的近期细节
- `contextSummary`：压缩过往 observation，保留论文 ID、URL、失败和未完成事项
- SQLite Memory：保存跨任务仍有价值的稳定信息

OpenAI 模式使用模型生成阶段摘要；压缩调用失败时退回确定性 JSON 摘要，因此 continuation 不依赖额外一次模型调用必然成功。

## Metrics 与轨迹

每次运行的 JSONL 轨迹保存在 `.agent-data/runs/<run-id>.jsonl`，敏感字段在写入前脱敏。跨运行指标保存在 `.agent-data/metrics.db`，CLI 会根据历史 Run 计算累计任务成功率。`metrics_recorded` 事件包含：

```text
success
duration_ms
llm_calls
input_tokens / output_tokens / total_tokens
tool_calls / tool_failures / tool_duration_ms
human_approval_requests
context_compactions
goal_turns
cache_read_input_tokens / cache_creation_input_tokens
```

CLI 会在任务结束时输出这些核心指标。Token 指标依赖 Provider 返回的 usage；离线 `DemoModel` 的 token 数为 0。Session L3 摘要是独立模型调用，其调用数、Token 和延迟记录在 `sessions.db`，不会从成本口径中消失。

## 理论映射

| Runtime 机制 | 对应理论与论文 |
| --- | --- |
| 调度步骤内的原生 `tool_call -> tool_result` | ReAct：把推理和行动组成反馈闭环 |
| 工具选择、schema 与宿主执行 | [Toolformer](../../ai-agent-roadmap-site/assets/pdfs/toolformer.pdf)：工具调用是模型策略，但权限属于运行时 |
| 结构化计划与依赖校验 | [Understanding the Planning of LLM Agents](../../ai-agent-roadmap-site/assets/pdfs/understanding-the-planning-of-llm-agents.pdf)：计划生成之后还需要监控和验证 |
| scope、状态、来源和检索预算 | [Memory in the LLM Era](../../ai-agent-roadmap-site/assets/pdfs/memory-in-the-llm-era.pdf) 与 [A-MEM](../../ai-agent-roadmap-site/assets/pdfs/a-mem-agentic-memory.pdf)：记忆需要生命周期，不是 append-only 日志 |
| recent / summary / long-term 三层上下文 | [HiAgent](../../ai-agent-roadmap-site/assets/pdfs/hiagent.pdf)：长任务需要层级工作记忆 |
| Token、延迟、工具和压缩指标 | [Toward Efficient Agents](../../ai-agent-roadmap-site/assets/pdfs/toward-efficient-agents.pdf)：在成功率与计算成本之间联合优化 |

这些论文提供设计原则，代码实现仍采用可验证的工程约束。例如，模型负责提出计划，但 DAG 合法性由程序判断；模型建议调用工具，但工具权限、参数和超时由宿主控制。

## 目录

| 文件 | 职责 |
| --- | --- |
| `cmd/your-agent/main.go` | CLI、Provider 配置、人工审批与指标输出 |
| `cmd/your-agent-server/main.go` | 有界 HTTP 队列、持久任务、可恢复 SSE、Session API、文件工作台、WebSocket PTY、取消和审批 |
| `cmd/feishu-adapter/main.go` | 飞书回调 sidecar |
| `internal/agent/agent.go` | Scheduler 主执行链、Provider 原生 ReAct、Goal continuation、上下文压缩和停止条件 |
| `internal/session/` | 完整会话持久化、L0/L1/L2/L3 送模视图压缩、摘要预热与超限恢复 |
| `internal/subagent/` | 子 Agent 持久生命周期与独立工具化 Runtime |
| `internal/server/` | 有界任务调度、任务恢复、可恢复 SSE、Session/文件/子 Agent API、Web UI 与 PTY 协议 |
| `internal/integrations/feishu/` | 飞书事件、图片、卡片、映射和 API Client |
| `internal/ui/ui.go` | 并发安全终端输出、spinner、审批和交互命令 |
| `internal/provider/openai.go` | OpenAI-compatible Responses Provider |
| `internal/tools/` | 原生工具 Registry、语义代码搜索、Skills 与生命周期子 Agent 工具 |
| `internal/skills/` | 仓库、项目和用户级 Markdown Skills |
| `internal/subagent/` | 子 Agent 父子关系、状态、超时、取消和 SQLite 持久化 |
| `internal/verification/` | 文件修改后的宿主验证门禁 |
| `internal/model/` | Demo、OpenAI Planner 与原生步骤执行 |
| `internal/planning/` | DAG 校验、SQLite 状态、依赖调度、Verifier 与人工验收 |
| `internal/plugin/` | 命令插件与 MCP stdio 集成 |
| `internal/goal/` | 独立 Goal 生命周期与 continuation |
| `internal/input/`、`internal/render/`、`internal/tui/` | readline、Markdown 与全屏 TUI |
| `internal/memory/store.go` | SQLite 记忆生命周期与预算检索 |
| `internal/metrics/store.go` | Run 指标持久化与累计任务成功率 |
| `internal/evaluator/report.go` | 确定性报告验收条件 |
| `internal/trajectory/logger.go` | 并发安全、可脱敏的 JSONL 轨迹 |
| `internal/app/factory.go` | 运行时装配与资源生命周期 |

## 参数

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

`token-budget=0` 和 `goal-turns=0` 均表示不设置本地上限；每个 Goal turn 仍受 `max-steps` 控制。`approval=allow` 只适合受控自动化环境。

## 验证

需要 Go 1.25 或更高版本。SQLite 使用纯 Go 的 `modernc.org/sqlite` 驱动，Windows 构建不再需要 CGO 或 C 编译器。

```bash
GOCACHE="$PWD/.gocache" go test ./...
```

仓库根目录还提供：

```bash
make e2e                 # CLI + HTTP 真实进程回归
make browser-regression  # Playwright 桌面与移动端回归
make open-source-check   # secret、格式、vet、测试、内容检查
make release-snapshot    # 本机归档与 SHA-256；安装 cosign 时同时签名
make verify-release
```

GitHub Actions 在 Linux、macOS、Windows 原生构建和测试；`v*` Tag 会归档三套二进制、生成校验和，并用 GitHub OIDC + Cosign 进行 keyless 签名。

测试覆盖离线完整轨迹、OpenAI 请求与图片/usage 解析、完整块中断挽救、只读工具有序并行、截断 turn 续跑、计划依赖环、工具白名单/审批/超时/输出预算、SQLite scope/归档/检索预算、Session L0/L1/L2/L3 压缩、异步摘要失败降级和完整历史保留、Session 保存与 Fork、HTTP 跨轮继续、飞书事件/图片/卡片/审批、累计任务成功率，以及跨 3 个 Goal turn 的 continuation 和上下文压缩。

当前 OpenAI 路径使用 Responses API 原生 function-call event，保留 Provider call ID，并把匹配的 tool result 作为原生 block 回送。模型只能提议工具与参数；schema、权限、审批、超时、输出预算和 Verification Gate 仍由宿主掌握。

服务端将异步任务持久化到 `tasks.db`，重启后会把未完成任务标记为 `interrupted`。Session REST API 支持列表、创建、改名、Fork、删除、结构化消息和 canonical events；Web 工作台还提供受工作区边界保护的文件浏览/编辑/下载，以及带同源校验和进程回收的 WebSocket PTY。
