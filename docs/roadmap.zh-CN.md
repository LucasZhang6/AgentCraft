# 能力图谱与实现里程碑

[English](roadmap.md) | [简体中文](roadmap.zh-CN.md)

项目按能力依赖组织资料，但不规定固定课程或阅读周期。每个主题同时对应研究问题、代表论文和可验证的工程对象。

| 层级 | 主题 | 代表论文 | 工程对象 |
| --- | --- | --- | --- |
| 1 | Transformer 与上下文能力 | Attention Is All You Need、GPT-3 | Model Provider、上下文组装、模型路由 |
| 2 | 推理、检索与对齐 | CoT、RAG、InstructGPT、Constitutional AI | 检索器、引用、策略规则、结构化输出 |
| 3 | Agent Loop | Autonomous Agents Survey、Rise and Potential | Controller、状态、动作协议、停止条件 |
| 4 | Memory | Memory in the LLM Era、A-MEM | 会话状态、长期记忆、来源、更新与删除 |
| 5 | Tool Use | Toolformer、AutoGen | Tool Registry、schema、权限、超时、错误返回 |
| 6 | Planning | Understanding LLM Agent Planning、CoT | 计划步骤、依赖、预算、验收标准 |
| 7 | Evaluation / Safety | SPA-Bench、CyBench、AgentPoison | 真实任务集、权限矩阵、审计与安全拦截 |
| 8 | Optimization / Productization | DPO、Search-R1、AgentGym-RL、AFlow | 轨迹、偏好对、技能库、workflow 优化 |

## 项目里程碑

当前参考实现已覆盖真实 Provider 流式输出、模型回退与 Prompt Cache，文件/Shell/搜索/网页/澄清/子 Agent/插件/MCP 工具，结构化 Session、独立 Goal、持久 DAG Planning、SQLite Memory、L0/L1/L2/L3 压缩、CLI/TUI/Web/飞书入口，以及跨平台构建、E2E、浏览器和发布回归。

下一阶段重点是受控 PDF 解析和引用核验、Memory 候选确认与冲突处理、公开真实任务评估集、生产级进程/网络/租户隔离、飞书长连接与加密回调，以及从多次验收轨迹中提取可版本化 Skill。

新增论文或实现时，应明确它改变了哪一层、依赖哪些前置能力、增加了什么成本和攻击面，以及如何用可复现任务验证其价值。
