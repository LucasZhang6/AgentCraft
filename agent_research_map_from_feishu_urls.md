# Agent Research Map From Feishu URL List

[English](agent_research_map_from_feishu_urls.md) | [简体中文](agent_research_map_from_feishu_urls.zh-CN.md)

Source URL list: `feishu_agent_urls.md`

Scope: organize the source URLs into a research map for designing and implementing Agent systems, rather than preserving them as a flat bibliography.

## 0. How To Read This Collection

The Feishu document mixes surveys, classic LLM foundations, memory papers, tool-use papers, multi-agent papers, embodied/multimodal agents, RL training, benchmarks, and project repositories. For engineering practice, the most useful mental model is:

`LLM capability -> Agent loop -> Memory -> Tools -> Planning -> Evaluation -> Optimization -> Productization`

An Agent is not just an LLM wrapper. It is a system that repeatedly observes state, reasons or plans, calls tools, writes or retrieves memory, evaluates progress, and decides when to stop. Most papers in the list improve one of these decisions.

## 1. Foundation Layer

Read these first if you want the theory to be solid.

### Attention Is All You Need

URL: https://proceedings.neurips.cc/paper/2017/file/3f5ee243547dee91fbd053c1c4a845aa-Paper.pdf

Core idea: Transformer replaces recurrence with self-attention. For Agents, this explains why long context is powerful but expensive: every step adds tokens that future steps must attend to.

Engineering takeaway: context is a scarce resource. Agent design must actively compress, retrieve, and discard information.

### Language Models Are Few-Shot Learners

URL: https://proceedings.neurips.cc/paper_files/paper/2020/file/1457c0d6bfcb4967418bfb8ac142f64a-Paper.pdf

Core idea: large models can adapt through prompting without gradient updates. This is the root of prompt-driven agents.

Engineering takeaway: before fine-tuning, try task framing, examples, tool schemas, and structured output.

### Training Compute-Optimal Large Language Models

URL: https://arxiv.org/pdf/2203.15556

Core idea: model quality depends on compute/data scaling trade-offs. This matters because many Agent papers compare model size, rollout cost, and token budget.

Engineering takeaway: do not assume a larger model is always the best Agent controller. Sometimes a smaller model plus retrieval/tools wins.

### GShard and Switch Transformers

URLs:
- https://arxiv.org/pdf/2006.16668
- https://www.jmlr.org/papers/volume23/21-0998/21-0998.pdf

Core idea: mixture-of-experts and conditional computation scale models by activating only part of the network.

Engineering takeaway: Agent systems can mirror this idea at system level: route easy tasks to cheap paths and hard tasks to expensive paths.

### LoRA and Mixed Precision

URLs:
- https://arxiv.org/pdf/2106.09685v1/1000
- https://arxiv.org/pdf/1710.03740

Core idea: reduce adaptation or training cost.

Engineering takeaway: useful when you later specialize a model for tool calling, planning style, or domain-specific instructions.

### CLIP, Flamingo, PaLM-E

URLs:
- http://proceedings.mlr.press/v139/radford21a/radford21a.pdf
- https://proceedings.neurips.cc/paper_files/paper/2022/file/960a172bc7fbf0177ccccbb411a7d800-Paper-Conference.pdf
- https://openreview.net/pdf?id=VTpHpqM3Cf

Core idea: connect language with visual or embodied signals.

Engineering takeaway: if your future Agent needs screenshots, GUI control, robotics, video, or documents, multimodal grounding is not optional.

### Chain-of-Thought, RAG, Toolformer, RLHF, Constitutional AI

URLs:
- https://proceedings.neurips.cc/paper_files/paper/2022/file/9d5609613524ecf4f15af0f7b31abca4-Paper-Conference.pdf
- https://proceedings.neurips.cc/paper/2020/file/6b493230205f780e1bc26945df7481e5-Paper.pdf
- https://proceedings.neurips.cc/paper_files/paper/2023/file/d842425e4bf79ba039352da0f658a906-Paper-Conference.pdf
- https://arxiv.org/pdf/2203.02155
- https://arxiv.org/pdf/2212.08073

Core idea: reasoning traces, external knowledge, tool invocation, instruction alignment, and safety constraints are the direct ancestors of modern Agents.

Engineering takeaway: a practical Agent usually combines all five: prompt reasoning, retrieval, tools, aligned behavior, and guardrails.

## 2. Agent Survey Layer

These papers give the map before you dive into modules.

### Toward Efficient Agents

URLs:
- https://arxiv.org/abs/2601.14192
- https://efficient-agents.github.io/
- https://github.com/yxf203/Awesome-Efficient-Agents

Core idea: efficient Agents are optimized across memory, tool use, and planning, not just model size. Efficiency means better performance under fixed cost or lower cost at similar performance.

Engineering takeaway: when building your own Agent, log token cost, tool calls, latency, retries, and success rate from day one.

### Memory in the LLM Era

URLs:
- https://arxiv.org/abs/2604.01707
- https://github.com/Yanchen398/Memory-in-the-LLM-Era

Core idea: memory systems can be modular: short-term state, long-term storage, retrieval, summarization, and update policies.

Engineering takeaway: separate conversation state, user profile, task memory, and reusable skills instead of dumping everything into one vector database.

### Externalization in LLM Agents

URL: https://arxiv.org/pdf/2604.08224

Core idea: Agents externalize capability into memory, skills, protocols, and harness engineering.

Engineering takeaway: much of Agent quality comes from the harness: tool schema, execution sandbox, state machine, retry policy, and observability.

### Agent System Surveys

URLs:
- https://huggingface.co/papers/2601.02749
- http://arxiv.org/abs/2601.01743v1
- https://arxiv.org/pdf/2401.03568
- https://xuanjing-huang.github.io/files/agent.pdf
- https://link.springer.com/content/pdf/10.1007/s11704-024-40231-1.pdf
- https://arxiv.org/pdf/2402.01680
- https://arxiv.org/pdf/2401.05459
- https://arxiv.org/pdf/2402.02716

Core idea: these surveys cover autonomous agents, multimodal interaction, multi-agent systems, personal agents, and planning.

Engineering takeaway: use them to choose your Agent type: personal assistant, research agent, coding agent, GUI agent, domain expert, or multi-agent workflow.

## 3. Memory Layer

Memory is the first major capability to master because it determines whether your Agent improves over time.

### MemoryBank, ExpeL, MemoChat

URLs:
- https://github.com/zhongwanjun/MemoryBank-SiliconFriend
- https://github.com/melih-unsal/demogpt
- https://github.com/woooodyy/llm-agent-paper-list

Core idea: early Agent memory stores conversations, experiences, and reusable insights.

Engineering takeaway: start with simple memory: save user facts, task outcomes, and failure notes. Then add scoring and retrieval.

### A-MEM

URLs:
- https://arxiv.org/pdf/2502.12110
- https://github.com/WujiangXu/A-mem
- https://github.com/WujiangXu/A-mem-sys

Core idea: dynamic memory organization and memory evolution improve multi-hop reasoning while reducing answer tokens.

Engineering takeaway: do not treat memory as append-only. Add update, merge, delete, and relation-building operations.

### Mem0, PlugMem, Lightweight Memory

URLs:
- https://arxiv.org/abs/2603.03296
- https://github.com/TIMAN-group/PlugMem
- https://arxiv.org/pdf/2604.07798
- https://github.com/Shichun-Liu/Agent-Memory-Paper-List

Core idea: production memory needs lightweight extraction, storage, retrieval, and low-latency update.

Engineering takeaway: for a personal Agent, memory quality matters more than memory quantity.

### HiAgent, STMA, Optimus-1, VideoAgent

URLs:
- https://arxiv.org/pdf/2408.09559
- https://github.com/HiAgent2024/HiAgent
- https://arxiv.org/pdf/2502.10177
- https://github.com/SP4595/STMA-A-Spatio-Temporal-Memory-Agent-for-Long-Horizon-Embodied-Task-Planning/blob/main/eb-example.pdf
- https://proceedings.neurips.cc/paper_files/paper/2024/file/5949a8750a110ce1f0631b1776c500a2-Paper-Conference.pdf
- https://github.com/JiuTian-VL/Optimus-1
- https://arxiv.org/pdf/2403.11481
- https://github.com/YueFan1014/VideoAgent

Core idea: long-horizon tasks need hierarchical, spatial-temporal, or multimodal memory.

Engineering takeaway: if your Agent works over long tasks, store task state at multiple granularities: latest step, episode summary, long-term lessons.

### Multi-Agent Memory: SRMT and MIRIX

URLs:
- https://arxiv.org/pdf/2501.13200
- https://github.com/Aloriosa/srmt
- https://arxiv.org/pdf/2507.07957
- https://github.com/Mirix-AI/MIRIX

Core idea: multiple agents need shared and local memory.

Engineering takeaway: shared memory should contain verified facts and decisions; local memory should contain role-specific working context.

### AgentPoison

URLs:
- https://proceedings.neurips.cc/paper_files/paper/2024/file/eb113910e9c3f6242541c1652e30dfd6-Paper-Conference.pdf
- https://github.com/AI-secure/AgentPoison

Core idea: memory or knowledge-base poisoning can compromise Agents.

Engineering takeaway: memory must have provenance, trust level, review state, and deletion controls.

## 4. Tool Use And Workflow Layer

Tools turn an LLM from a text generator into an actor.

### Toolformer

URLs:
- https://proceedings.neurips.cc/paper_files/paper/2023/file/d842425e4bf79ba039352da0f658a906-Paper-Conference.pdf
- https://github.com/lucidrains/toolformer-pytorch

Core idea: language models can learn when and how to call tools.

Engineering takeaway: tool-use examples should teach both invocation timing and parameter formatting.

### AFlow, MetaGPT, AutoGen, CAMEL, ChatDev

URLs:
- https://arxiv.org/abs/2410.10762
- https://github.com/foundationagents/aflow
- https://github.com/FoundationAgents/MetaGPT
- https://arxiv.org/pdf/2308.08155
- https://github.com/microsoft/autogen
- https://proceedings.neurips.cc/paper_files/paper/2023/file/a3621ee907def47c1b952ade25c67698-Paper-Conference.pdf
- https://github.com/camel-ai/camel
- https://github.com/openbmb/chatdev

Core idea: Agent workflow can be manually designed, generated, or organized as multi-agent collaboration.

Engineering takeaway: for your first system, build a single-agent workflow with explicit tools before adding multiple agents.

### REGENT, SPA-Bench, CyBench

URLs:
- https://arxiv.org/abs/2412.04759
- https://openreview.net/forum?id=ikXjMk8RUs
- https://github.com/ai-agents-2030/SPA-Bench
- https://arxiv.org/abs/2408.08926
- https://github.com/andyzorigin/cybench

Core idea: tool and environment Agents must be evaluated in realistic tasks.

Engineering takeaway: benchmark your Agent with real tasks, not just friendly demos.

## 5. Planning Layer

Planning controls how much the Agent thinks before acting.

### Understanding LLM Agent Planning

URL: https://arxiv.org/pdf/2402.02716

Core idea: planning involves decomposition, search, reflection, correction, and execution monitoring.

Engineering takeaway: give your Agent an explicit planner module, but keep it budget-aware.

### ReWOO, HuggingGPT, Graph-Based Planning

URLs:
- https://arxiv.org/pdf/2308.00352
- https://github.com/FoundationAgents/MetaGPT
- https://arxiv.org/abs/2406.07155
- https://github.com/openbmb/chatdev
- https://github.com/ulab-uiuc/GraphPlanner

Core idea: decompose tasks into steps, dependencies, roles, or graphs.

Engineering takeaway: represent plans as structured JSON or DAGs, not prose paragraphs only.

### Efficient Planning And Search

URLs:
- https://opencausalab.github.io/DEPO
- https://github.com/jiajingyyyyyy/AutoTool
- https://github.com/amazon-science/EvoMAS
- https://github.com/Gen-Verse/LatentMAS

Core idea: modern Agent planning increasingly optimizes cost, communication, and search depth.

Engineering takeaway: log failed branches and retry causes. This gives you data for pruning and future skill creation.

## 6. RL And Post-Training Layer

This is for when prompting and workflow engineering are no longer enough.

### PPO, DPO, DeepSeek-R1, Search-R1

URLs:
- https://arxiv.org/pdf/2203.02155
- https://arxiv.org/pdf/2305.18290
- https://arxiv.org/pdf/2501.12948
- https://arxiv.org/pdf/2503.09516
- https://github.com/PeterGriffinJin/Search-R1

Core idea: reinforcement and preference learning can shape reasoning and tool-use behavior.

Engineering takeaway: before training an Agent, define reward signals: task success, tool cost, latency, safety, and user satisfaction.

### AgentGym-RL, Agent-R1, SkillRL, ProRL, AgentV-RL, MemPO

URLs:
- https://openreview.net/forum?id=ZgCCDwcGwn
- https://github.com/WooooDyy/AgentGym-RL
- https://agentgym-rl.github.io/
- https://arxiv.org/pdf/2511.14460
- https://github.com/AgentR1/Agent-R1
- https://github.com/aiming-lab/SkillRL
- https://arxiv.org/abs/2603.18815
- https://github.com/NVIDIA-NeMo/ProRL-Agent-Server
- https://arxiv.org/abs/2604.16004
- https://github.com/JiazhengZhang/AgentV-RL
- https://arxiv.org/abs/2603.00680
- https://github.com/TheNewBeeKing/MemPO

Core idea: Agent RL optimizes multi-turn behavior, long-horizon decisions, memory use, skill reuse, and verifier feedback.

Engineering takeaway: collect trajectories first. Training comes later. Good logs are the seed of Agent RL.

## 7. Multimodal, GUI, And Embodied Agent Layer

Read this layer after you know the core Agent loop.

### ShowUI, Magma, Optimus-2, MobileAgent

URLs:
- https://github.com/showlab/ShowUI
- https://openaccess.thecvf.com/content/CVPR2025/papers/Lin_ShowUI_One_Vision-Language-Action_Model_for_GUI_Visual_Agent_CVPR_2025_paper.pdf
- https://microsoft.github.io/Magma/
- https://arxiv.org/abs/2502.13130
- https://cybertronagent.github.io/Optimus-2.github.io/
- https://arxiv.org/abs/2502.19902
- https://github.com/X-PLUG/MobileAgent

Core idea: Agents can act in GUI, Minecraft, mobile devices, and multimodal environments.

Engineering takeaway: GUI Agents need perception, action primitives, state tracking, and recovery from visual errors.

### VADAR, GUI-Xplore, SpiritSight, Feature4X

URLs:
- https://github.com/damianomarsili/VADAR
- https://arxiv.org/abs/2502.06787
- https://arxiv.org/html/2503.17709v1
- https://arxiv.org/html/2503.03196v1
- https://feature4x.github.io/

Core idea: visual and spatial Agents need dynamic APIs, exploration, and robust scene representation.

Engineering takeaway: for real GUI or visual Agents, design observation parsing before planning.

### Video And 3D Agent Papers

URLs:
- https://zhengrongz.github.io/AoTD/
- https://milvlg.github.io/videoarm/
- https://worldmm.github.io/
- https://ego2web.github.io/
- https://vinedresser3d.github.io/
- https://changyueshi.github.io/REALM

Core idea: video and 3D Agents rely heavily on memory and tool-guided seeking.

Engineering takeaway: long video Agents are memory systems with visual retrieval, not just VLM prompts.

## 8. Evaluation And Safety Layer

This layer prevents you from building a demo that looks good but is unreliable.

### Benchmarks

URLs:
- https://openreview.net/forum?id=ikXjMk8RUs
- https://github.com/ai-agents-2030/SPA-Bench
- https://github.com/andyzorigin/cybench
- https://github.com/tb6147877/TowerMind
- https://github.com/LivXue/SoMe
- https://github.com/DISL-Lab/BRIDGE-Benchmark

Core idea: different Agents need different benchmarks: smartphone, cybersecurity, games, social media, retrieval, GUI, and research tasks.

Engineering takeaway: create a private benchmark of 30 to 100 tasks for your own Agent before optimizing.

### Safety And Robustness

URLs:
- https://arxiv.org/pdf/2202.03286
- https://github.com/google-research/google-research
- https://github.com/AI-secure/AgentPoison
- https://github.com/HASHIRU-AI/NAAMSE
- https://github.com/zpforlove/Resp-Agent

Core idea: Agents expand the attack surface because they can store, retrieve, call tools, and act.

Engineering takeaway: use permission gates for high-impact tools, sandbox execution, memory provenance, and audit logs.

## 9. Recommended 10-Week Learning Path

Week 1: Transformer, GPT-3, CoT, RAG, Toolformer.

Week 2: read two broad Agent surveys and draw the Agent loop.

Week 3: implement a minimal ReAct-style Agent with search, file read, and calculator tools.

Week 4: add structured tool schemas, JSON plans, retries, and execution logs.

Week 5: read memory papers; implement user memory, task memory, and failure memory.

Week 6: add retrieval scoring, memory update/merge/delete, and provenance.

Week 7: read planning papers; add task decomposition and budget-aware stopping.

Week 8: read AutoGen, MetaGPT, CAMEL; try a small multi-agent workflow only after the single-agent version is stable.

Week 9: read benchmark and safety papers; build an evaluation set and permission system.

Week 10: choose one specialization: personal research assistant, coding agent, GUI agent, paper-reading agent, or domain expert.

## 10. Personal Agent Architecture To Build

Start with this practical architecture:

1. Controller: decides `answer`, `retrieve`, `plan`, `tool_call`, `ask_user`, or `stop`.
2. Planner: produces a short structured plan with dependencies and budget.
3. Tool layer: typed tools with schema validation, sandboxing, timeouts, and permission levels.
4. Memory layer: short-term context, long-term user memory, task memory, tool-use memory, and failure memory.
5. Evaluator: checks factuality, task completion, cost, and safety.
6. Logger: stores full trajectories, tool calls, tokens, latency, errors, and final outcome.
7. Skill library: converts repeated successful workflows into reusable procedures.

The first version should be boring and observable. A reliable Agent beats a mysterious clever one.

## 11. Minimum Reading Set

If you only read 20 items from the parsed links, prioritize:

1. Attention Is All You Need
2. Language Models Are Few-Shot Learners
3. Chain-of-Thought Prompting
4. Retrieval-Augmented Generation
5. Toolformer
6. Training Language Models to Follow Instructions with Human Feedback
7. Toward Efficient Agents
8. Memory in the LLM Era
9. Externalization in LLM Agents
10. Understanding the Planning of LLM Agents
11. A Survey on LLM-Based Autonomous Agents
12. Large Language Model Based Multi-Agents
13. A-MEM
14. HiAgent
15. AgentPoison
16. AutoGen
17. MetaGPT
18. CAMEL
19. AgentGym-RL
20. Search-R1 or Agent-R1

## 12. What To Ignore At First

Do not start with every CVPR multimodal Agent paper. Many are valuable but domain-specific. For a personal Agent, first master text Agent fundamentals, memory, tool calling, planning, evaluation, and safety. Then branch into GUI, video, robotics, or multi-agent systems.
