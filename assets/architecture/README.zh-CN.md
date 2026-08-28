# 架构图资产

[English](README.md) | [简体中文](README.zh-CN.md)

本目录保存 README 和设计文档使用的 ImageGen 架构图。图片通过 Codex 内置 `image_gen` 工具生成，再复制到仓库中；项目不依赖本地临时目录或外部图片地址。

| 文件 | 表达内容 | 使用位置 |
| --- | --- | --- |
| `agent-capability-roadmap.png` | Agent 能力从 LLM 基础到产品化的演进路线 | 根 README |
| `agent-runtime-architecture.png` | Runtime 控制平面、闭环与数据平面 | 根 README、`docs/architecture.md` |
| `your-agent-pipeline.png` | Your Agent 的论文处理流水线 | 根 README |
| `minimal-agent-loop.png` | 最小 Agent 决策与行动闭环 | 根 README、示例 README |
| `agent-replayable-trajectory.png` | 单次运行的可回放轨迹 | 根 README |

## 统一视觉提示

```text
Create a clean, high-resolution 16:9 technical infographic for open-source
documentation. Use a white background, strict grid, crisp vector-like lines,
generous spacing, and a restrained charcoal, teal, coral, mustard, and light-gray
palette. Keep every supplied label exact and legible. Use only the stated nodes
and directional connections. Avoid logos, watermarks, 3D, gradients, heavy
shadows, decorative characters, rounded pills, crossed arrows, and extra text.
```

## 各图内容提示

### Agent 能力演进路线

```text
Title: "AI Agent 能力演进路线"
Create one continuous left-to-right flow with these exact stages:
"1 LLM 基础能力" -> "2 Agent Loop" -> "3 Memory" -> "4 Tool Use" ->
"5 Planning" -> "6 Evaluation / Safety" -> "7 RL Optimization" ->
"8 Productization".
```

### 专属 Agent Runtime 架构

```text
Title: "专属 Agent Runtime 架构"
Use two bands: "CONTROL PLANE" and "DATA PLANE".
Control plane: User Goal -> Controller -> Tool Layer -> Environment ->
Observation -> Evaluator. Planner and Memory connect bidirectionally to
Controller. Evaluator returns to Controller through a connector labeled
"complete / retry".
Data plane: Logger -> Trajectory Store -> Skill Library.
Do not draw connectors across the band separator.
```

### Your Agent 处理流水线

```text
Title: "Your Agent 处理流水线"
Paper URL -> Metadata / PDF / Repository Tools -> Paper Type Detection ->
Structured Plan. Branch into Problem, Method, Evidence, and Limitations. Merge
into Citation & Completeness Evaluation. Output Structured Result and Preference
& Failure Memory; memory then feeds Reusable Paper Analysis Skill.
```

### Minimal Agent Loop

```text
Title: "Minimal Agent Loop"
Goal -> Plan -> Decide -> Tool -> Observation -> Evaluate. From Evaluate, a
"continue" branch returns to Decide and a "complete" branch exits to Stop.
Memory connects bidirectionally to Decide and Evaluate.
```

### Agent 可回放轨迹

```text
Title: "Agent 可回放轨迹"
Enclose one ordered stream with the label "One Replayable Run": Goal -> Plan ->
Model Decisions -> Tool Calls -> Observations -> Memory Reads / Writes -> Final
Result -> Evaluator Judgment -> Cost & Latency. Use exactly nine checkpoints and
do not branch.
```

生成模型可能在文字或连线细节上产生偏差。提交新版本前，应人工检查标签、箭头方向和模块关系，并重新运行仓库的 Markdown 图片链接检查。
