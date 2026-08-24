# 基于飞书 URL 列表的 Agent 研究图谱

[English](agent_research_map_from_feishu_urls.md) | [简体中文](agent_research_map_from_feishu_urls.zh-CN.md)

来源索引：`feishu_agent_urls.zh-CN.md`

本文把飞书页面中的论文、项目和综述从平铺链接整理为 Agent 系统图谱。完整飞书原文镜像见 [`docs/feishu-agent-papers-and-projects.zh-CN.md`](docs/feishu-agent-papers-and-projects.zh-CN.md)，31 篇代表论文的双语详解见交互站点。

## 0. 如何使用这份图谱

飞书合集混合了 LLM 基础、Memory、Tool Use、多 Agent、具身/多模态、RL、Benchmark 和工程项目。更适合工程理解的主线是：

`LLM 基础 -> Agent Loop -> Memory -> Tool Use -> Planning -> Evaluation/Safety -> RL 优化 -> 工程化`

Agent 不是 LLM 的包装层，而是不断观察状态、规划或决策、调用工具、读取反馈、更新状态并判断停止的系统。阅读每篇论文时，应继续追问它改变了哪个模块、增加了什么成本或攻击面，以及如何验证收益。

## 1. 基础能力层

### Transformer 与上下文

- [Attention Is All You Need](https://proceedings.neurips.cc/paper/2017/file/3f5ee243547dee91fbd053c1c4a845aa-Paper.pdf)：Self-Attention 奠定现代 LLM 架构，也解释长 Agent 轨迹为何昂贵。
- [Language Models are Few-Shot Learners](https://proceedings.neurips.cc/paper_files/paper/2020/file/1457c0d6bfcb4967418bfb8ac142f64a-Paper.pdf)：In-context Learning 是 Prompt 驱动 Agent 的能力基础。
- [Training Compute-Optimal Large Language Models](https://arxiv.org/pdf/2203.15556)：模型、数据和算力存在权衡；最大模型不一定是最经济的 Controller。

### 条件计算与低成本适配

- [GShard](https://arxiv.org/pdf/2006.16668)
- [Switch Transformers](https://www.jmlr.org/papers/volume23/21-0998/21-0998.pdf)
- [LoRA](https://arxiv.org/pdf/2106.09685v1/1000)
- [Mixed Precision Training](https://arxiv.org/pdf/1710.03740)

这些工作主要解决模型扩展和适配成本。对应到 Agent 系统，可以把简单任务路由到便宜路径，把复杂任务升级到更强模型，并保留相同工具与验收边界。

### 多模态基础

- [CLIP](http://proceedings.mlr.press/v139/radford21a/radford21a.pdf)
- [Flamingo](https://proceedings.neurips.cc/paper_files/paper/2022/file/960a172bc7fbf0177ccccbb411a7d800-Paper-Conference.pdf)
- [PaLM-E](https://openreview.net/pdf?id=VTpHpqM3Cf)

GUI、截图、视频、机器人和复杂文档 Agent 不能只依赖文本，需要明确图像或环境 observation 如何进入模型、如何与动作对齐、如何被独立验证。

### 现代 Agent 的直接前身

- [Chain-of-Thought](https://proceedings.neurips.cc/paper_files/paper/2022/file/9d5609613524ecf4f15af0f7b31abca4-Paper-Conference.pdf)
- [Retrieval-Augmented Generation](https://proceedings.neurips.cc/paper/2020/file/6b493230205f780e1bc26945df7481e5-Paper.pdf)
- [Toolformer](https://proceedings.neurips.cc/paper_files/paper/2023/file/d842425e4bf79ba039352da0f658a906-Paper-Conference.pdf)
- [InstructGPT / RLHF](https://arxiv.org/pdf/2203.02155)
- [Constitutional AI](https://arxiv.org/pdf/2212.08073)

它们分别对应候选推理、外部证据、工具选择、偏好和原则约束。工程上仍要把自然语言输出转换成结构化计划、工具策略和可执行权限。

## 2. Agent 综述层

### 高效 Agent

- [Toward Efficient Agents](https://arxiv.org/abs/2601.14192)
- [项目页](https://efficient-agents.github.io/)
- [Awesome-Efficient-Agents](https://github.com/yxf203/Awesome-Efficient-Agents)

核心观点是效果优先地联合比较成功率、Token、延迟、工具调用、搜索深度和多 Agent 通信，而不是只追求减少某一项成本。

### Memory 综述

- [Memory in the LLM Era](https://github.com/Yanchen398/Memory-in-the-LLM-Era)
- [Awesome GraphMemory](https://github.com/DEEP-PolyU/Awesome-GraphMemory)

阅读 Memory 论文时应区分 Session 历史、工作上下文、长期事实、RAG 证据、计划状态和完整轨迹。它们可以共享数据库技术，但具有不同的生命周期与信任语义。

### Agent 系统综述

- [The Rise and Potential of LLM Based Agents](https://xuanjing-huang.github.io/files/agent.pdf)
- [LLM Agent Paper List](https://github.com/woooodyy/llm-agent-paper-list)
- [A Survey on LLM-based Autonomous Agents](https://link.springer.com/content/pdf/10.1007/s11704-024-40231-1.pdf)
- [LLM Agent Survey Resources](https://github.com/paitesanshi/llm-agent-survey)
- [Multi-Agent Survey Papers](https://github.com/taichengguo/llm_multiagents_survey_papers)

综述用于建立 profiling、perception、memory、planning、action 和 evaluation 坐标，不应被当成实现时必须堆齐的组件清单。

## 3. Memory 层

### 从历史记录到经验

MemoryBank、ExpeL、MemoChat 一类工作探索把对话、反馈和成功/失败轨迹转成未来可召回经验。工程上要保留来源、作用域、置信度和更新时间，并区分用户原话与模型派生结论。

### 动态组织与更新

- [A-MEM](https://arxiv.org/pdf/2502.12110)

A-MEM 强调记忆不是 append-only。新增信息可以建立链接、更新或合并旧内容。风险在于自动改写会传播模型错误，因此需要版本、软删除、候选确认和回放。

### 轻量 Memory 与检索预算

Mem0、PlugMem 等工作关注低成本写入和召回。是否需要向量或图结构应由真实召回失败决定；第一版可以从 SQLite 生命周期、关键词、Scope 和条数/字节预算开始。

### 层级工作记忆

- [HiAgent](https://arxiv.org/pdf/2408.09559)

HiAgent、STMA、Optimus-1、VideoAgent 等长任务工作说明近期细节、阶段摘要和长期状态应分层。原始历史仍需保留，压缩只改变送模视图。

### 多 Agent Memory

SRMT、MIRIX 一类研究讨论共享记忆、角色记忆和跨 Agent 信息流。需要特别关注租户/角色作用域、写入权限、冲突和通信成本。

### Memory 安全

- [AgentPoison](https://proceedings.neurips.cc/paper_files/paper/2024/file/eb113910e9c3f6242541c1652e30dfd6-Paper-Conference.pdf)

长期记忆可以成为延迟攻击面。网页、工具、共享空间写入的候选默认低信任；召回结果只能提供证据，不能授予文件、Shell 或生产 API 权限。

## 4. Tool Use 与 Workflow 层

### 学习工具选择

- [Toolformer](https://proceedings.neurips.cc/paper_files/paper/2023/file/d842425e4bf79ba039352da0f658a906-Paper-Conference.pdf)

模型可学习何时调用、调用哪个工具以及参数形式；宿主仍要负责 allowlist、schema、风险、审批、超时、取消、输出限制、路径/网络边界和审计。

### 编排与多 Agent

- [AFlow](https://arxiv.org/pdf/2410.10762)
- [MetaGPT](https://arxiv.org/pdf/2308.00352)
- [AutoGen](https://arxiv.org/pdf/2308.08155)
- [CAMEL](https://arxiv.org/pdf/2303.17760)
- [ChatDev](https://arxiv.org/pdf/2307.07924)

AutoGen 强调消息与参与者协议，MetaGPT 强调 SOP 和结构化产物，AFlow 把 Workflow 作为可搜索对象。实践顺序应是先让流程可执行，再让结果可评估，最后才优化流程。

### 工具任务与 Benchmark

- REGENT：跨任务工具和环境适配
- [SPA-Bench](https://openreview.net/forum?id=QAs1sI4hZX)：移动端真实状态变化
- [CyBench](https://arxiv.org/pdf/2408.08926)：网络安全长程任务与风险

Benchmark 最重要的启发是验证环境结果，而不是听 Agent 自称成功。

## 5. Planning 层

### Planning 综述

- [Understanding the Planning of LLM Agents](https://arxiv.org/pdf/2402.02716)

规划包括分解、生成、行动选择、执行监控、失败恢复和重规划。工程计划至少需要 ID、依赖、工具、角色、状态、尝试、证据和成功条件。

### 工具图与外部 Planner

ReWOO、HuggingGPT 和图规划工作探索先计划后执行、模型路由和工具依赖。适合比较纯 ReAct、静态 Workflow 与持久 DAG 在成功、成本和恢复上的差异。

### 高效 Planning 与搜索

规划搜索会增加模型调用和分支。应先证明额外搜索改善真实任务，而不是只让计划更长；简单任务应允许跳过重型 Planner。

## 6. RL 与后训练层

### 从 PPO/DPO 到推理和搜索 RL

- [DPO](https://arxiv.org/pdf/2305.18290)
- [DeepSeek-R1](https://arxiv.org/pdf/2501.12948)
- [Search-R1](https://arxiv.org/pdf/2503.09516)

DPO 适合从“好轨迹/坏轨迹”学习偏好；R1 类工作说明可验证奖励能塑造推理和工具策略。奖励必须同时考虑正确性、成本和安全，避免冗长或危险捷径。

### 长程 Agent RL 与 Skill

- [AgentGym-RL](https://openreview.net/forum?id=ZgCCDwcGwn)
- [SkillRL](https://github.com/aiming-lab/SkillRL)

Agent-R1、ProRL、AgentV-RL、MemPO 等工作也围绕多轮轨迹、工具和 Memory 展开。进入 RL 前必须先有可重置环境、稳定 Evaluator、训练/评估隔离和结构化 Trace。

## 7. 多模态、GUI 与具身 Agent

- [ShowUI](https://github.com/showlab/ShowUI)
- [Magma](https://microsoft.github.io/Magma/)
- [VADAR](https://github.com/damianomarsili/VADAR)
- [Feature4X](https://feature4x.github.io/)

这类系统的 observation 可能是 DOM、截图、视频、深度或机器人状态，action 则是点击、拖动、导航和控制。必须通过动作后的真实环境状态判定成功，并记录截图/状态证据。

视频与 3D Agent 还需要处理长时序压缩、空间一致性、跨平台动作差异和高采样成本，不能把文本 Agent 的 Benchmark 直接外推。

## 8. Evaluation 与 Safety 层

### Benchmark

评估至少覆盖任务成功、引用/环境证据、Token、延迟、工具调用与失败、重试、Memory 命中、人工介入和安全拦截。固定任务集应包含信息不足、工具失败、审批拒绝、长上下文和污染 Memory。

### Safety 与鲁棒性

安全不是最终回复过滤器，而是 Tool、Memory、Session 和 Adapter 的基础字段。高风险动作需要最小权限、沙箱、审批、取消、审计和结果读回；外部内容始终是数据，不是指令或授权。

## 9. 建议的十周阅读路径

1. Transformer、GPT-3、CoT、RAG、Toolformer、RLHF/Constitutional AI。
2. 两篇 Agent 综述和 Toward Efficient Agents。
3. Memory in the LLM Era、A-MEM、HiAgent、AgentPoison。
4. Toolformer 的工具视角与工具调用系统。
5. AutoGen、MetaGPT、AFlow，比较消息、SOP 与 Workflow 搜索。
6. Planning 综述、ReWOO/HuggingGPT 与 DAG 执行。
7. SPA-Bench、CyBench 与真实任务 Evaluator。
8. DPO、DeepSeek-R1、Search-R1。
9. AgentGym-RL、SkillRL 与结构化轨迹。
10. 复盘固定任务，决定自己的 Agent 需要哪些模块，而不是复制所有论文。

## 10. 建议构建的专属 Agent 架构

第一版建议包含：可替换 Provider、有取消和停止条件的 Controller、持久 Goal、结构化 Plan、受控 Tool Registry、分 Scope Memory、独立 Evaluator、结构化 Session/Trace/Metrics，以及至少一个真实用户入口。

模型负责提出计划和动作；宿主程序负责 schema、权限、超时、执行、持久化和验收。Session 保存全量消息，Goal 保存目标，Plan 保存执行状态，Memory 保存精选跨任务事实，Trace 保存回放证据。

## 11. 最小必读集

如果时间有限，优先阅读：Attention Is All You Need、GPT-3、CoT、RAG、Toolformer、Agent Survey、Toward Efficient Agents、Memory in the LLM Era、A-MEM、HiAgent、AgentPoison、Planning Survey、DPO、Search-R1、AgentGym-RL 和 SkillRL。

## 12. 初期可以暂缓的方向

不要在第一版就追求大量角色、复杂图 Memory、在线 RL、自动 Workflow 搜索或无限工具。先让单 Agent 闭环可观测、可取消、可恢复、可验收，并用真实任务证明每个新增模块的价值。
