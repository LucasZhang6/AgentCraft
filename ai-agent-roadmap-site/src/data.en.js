import { modules as sourceModules } from "./data.js";

const moduleTranslations = {
  "llm-foundation": {
    title: "LLM Foundations",
    subtitle: "From Transformer to alignment: the cognitive substrate of an agent.",
    summary:
      "This layer explains how a language model represents context, adapts from instructions and examples, externalizes intermediate reasoning, retrieves knowledge, calls APIs, and follows preference or principle-based constraints. These abilities do not create an agent by themselves, but every later loop, memory, tool, and optimization mechanism depends on them.",
    build:
      "Engineering focus: choose and route models deliberately, version prompts and schemas, assemble only relevant context, keep retrieval evidence separate from instructions, and record quality, token use, cache behavior, latency, and failure from the first run."
  },
  "agent-loop": {
    title: "Agent Loop",
    subtitle: "Turn model cognition into observed, stateful, and stoppable action.",
    summary:
      "An agent is not a single completion. It receives a goal, observes state, chooses or revises a plan, acts through a controlled capability, reads the real result, updates working state, and decides whether to continue. The loop makes failures observable and gives the system explicit places for recovery, memory, evaluation, and stopping.",
    build:
      "Engineering focus: define a small action protocol, explicit observations, cancellation and stop reasons, per-turn and per-goal budgets, durable state, and a replayable event stream before adding more autonomy or more agents."
  },
  memory: {
    title: "Memory",
    subtitle: "Preserve continuity without turning all history into permanent truth.",
    summary:
      "Memory design asks what should be retained, where it belongs, when it should be retrieved, how conflicts are revised, and when information should expire or be deleted. Session history, working context, long-term facts, task plans, and replay traces serve different owners and should not be collapsed into one vector index.",
    build:
      "Engineering focus: use scopes, provenance, confidence, lifecycle state, timestamps, update semantics, retrieval budgets, deletion, and trust boundaries. Keep complete history for audit while compressing only the model-facing view."
  },
  "tool-use": {
    title: "Tool Use",
    subtitle: "Let models propose actions while the host retains authority.",
    summary:
      "Tool use covers whether a capability is needed, which tool fits the information gap, how arguments are produced, how failures are interpreted, and whether the result justifies another action. Learned selection improves flexibility, but it does not provide filesystem, network, process, or business authorization.",
    build:
      "Engineering focus: register typed tools with schemas, risk levels, approval, timeout, bounded output, cancellation, audit, and environment-specific path or network policy. Return every success or failure as an observation."
  },
  planning: {
    title: "Planning",
    subtitle: "Represent long work as recoverable steps, dependencies, and evidence.",
    summary:
      "Planning connects an objective to executable progress. Useful plans expose subgoals, dependencies, available capabilities, risks, budgets, success criteria, monitoring, and replanning. Natural-language reasoning can propose a plan, but persistence and recovery require structured state owned by the runtime.",
    build:
      "Engineering focus: validate model-generated DAGs, persist step state, dispatch only ready dependencies, record attempts and evidence, verify outputs deterministically where possible, and make irreversible acceptance explicit."
  },
  "evaluation-safety": {
    title: "Evaluation / Safety",
    subtitle: "Judge real outcomes, evidence, cost, and authority rather than fluent claims.",
    summary:
      "Agent evaluation must ask whether the intended environment state was reached, what evidence supports the result, how much the trajectory cost, and whether any action crossed a permission boundary. Safety is therefore part of tool, memory, and state design rather than a final response filter.",
    build:
      "Engineering focus: maintain fixed real tasks, external verifiers, evidence fields, approval states, adversarial cases, failure categories, and metrics for success, cost, intervention, and blocked unsafe behavior."
  },
  "rl-optimization": {
    title: "RL Optimization",
    subtitle: "Improve policy from preferences, verified outcomes, and complete trajectories.",
    summary:
      "Preference optimization and agent RL can teach when to search, how long to reason, which tool sequence to choose, or which successful procedure to reuse. They only become meaningful after the environment, trace format, reward, and evaluation set are stable enough to distinguish genuine progress from reward exploitation.",
    build:
      "Engineering focus: collect versioned trajectories and paired preferences, define outcome and cost rewards, train in resettable sandboxes, compare against fixed baselines, and promote a policy or skill only after offline regression."
  },
  engineering: {
    title: "Personal Agent Engineering",
    subtitle: "Turn research modules into an observable, recoverable product.",
    summary:
      "Productization joins provider routing, a controller, durable goals and plans, controlled tools, lifecycle memory, independent evaluation, trace storage, user interfaces, and release gates. The objective is not to maximize component count; it is to produce a system whose behavior can be resumed, audited, limited, and improved.",
    build:
      "Engineering focus: Your Agent provides a Go reference runtime with offline and real providers, permissions, SQLite state, layered compaction, CLI/TUI/Web/Feishu surfaces, metrics, E2E tests, browser regression, and cross-platform release artifacts."
  }
};

const paperTranslations = {
  "llm-foundation": [
    {
      tags: ["Transformer", "Self-Attention", "Context"],
      overview:
        "The Transformer replaces recurrent sequence processing with self-attention, allowing every token to combine information from the rest of the sequence in parallel. It established the architecture behind modern LLMs and explains both the power and the cost of placing goals, tools, observations, plans, and memory in one context.",
      explanation: `Intuition: A recurrent model reads a sentence like a person passing one evolving note from word to word. A Transformer lays the page on a table so each token can directly inspect other tokens. Distant information can interact without traversing many recurrent steps, and training can process positions in parallel. The paper solves a sequence-modeling problem; it does not itself define an agent, memory lifecycle, or tool policy.

Method and evidence: The architecture uses scaled dot-product attention over queries, keys, and values. Multiple attention heads learn different relations, positional encodings restore order information, and residual, normalization, and feed-forward blocks stabilize representation. Translation experiments showed that recurrence and convolution were not required for strong quality and efficient training. Attention over a dense sequence grows approximately quadratically with length, which is a central reason long agent histories are expensive.

Agent relevance: A controller eventually represents instructions, tool schemas, plans, observations, retrieved facts, and prior messages as model-readable context. Self-attention lets the model connect these heterogeneous signals, so one LLM can coordinate many parts of a loop. It also exposes a systems problem: information being present does not guarantee that it will receive the right attention. Retrieval and compaction are mechanisms for selecting what the next decision should see.

Limits and implementation: Transformer does not guarantee factuality, executable plans, safe actions, or faithful reasoning. More context increases token cost and can dilute important evidence. An implementation should separate durable state from the prompt, retain provenance, summarize repeated observations, retrieve only relevant memory, and validate plans and tool arguments in code. Measure context length, latency, cache use, and task outcome instead of assuming a larger window is automatically better.`
    },
    {
      tags: ["GPT-3", "In-Context Learning", "Prompting"],
      overview:
        "GPT-3 showed that a sufficiently large autoregressive language model can adapt to many tasks from instructions and a few examples in its prompt, without updating parameters. That result enabled prompt programming and became the basis for describing agent roles, tools, output contracts, and demonstration trajectories in context.",
      explanation: `Intuition: Traditional task adaptation usually requires examples and another training run. GPT-3 demonstrated a different interface: describe the task and show a few input-output examples inside the current prompt, and the model attempts to continue the pattern. It resembles a broadly experienced worker learning a local procedure from a short briefing. No model weights change during this temporary adaptation.

Method and evidence: The paper trained autoregressive models up to 175 billion parameters and compared zero-shot, one-shot, and few-shot prompting across many language tasks. In-context performance often improved with scale, although results varied by task and remained below specialized systems in important cases. The work did not propose an Agent Loop; it established that a general language model can infer a temporary task from tokens supplied at inference time.

Agent relevance: Early agents rely on this ability everywhere. A system prompt describes responsibility, tool schemas act as an operating manual, output examples teach a decision format, and retrieved trajectories demonstrate behavior. The finding also explains why example order, ambiguous wording, or a poisoned retrieved passage can change policy: these are not passive documents but part of the model's current program.

Limits and implementation: GPT-3 remains sensitive to prompt form, can fabricate facts, inherits data bias, and is expensive at scale. Durable permissions, state transitions, and stop conditions should not live only in prose. Version prompt templates, test examples against a fixed task set, keep external evidence isolated from instructions, and parse structured output strictly. Use prompting for flexible policy proposals while retaining deterministic host contracts.`
    },
    {
      tags: ["Reasoning", "Chain of Thought", "Decomposition"],
      overview:
        "Chain-of-Thought prompting shows that including intermediate reasoning in few-shot demonstrations can improve arithmetic, commonsense, and symbolic tasks for sufficiently capable models. It made stepwise reasoning an accessible prompting interface and influenced later planning, reflection, and process-verification work.",
      explanation: `Intuition: A normal demonstration jumps from question to answer. A chain-of-thought demonstration includes the intermediate derivation, similar to asking a student to show working. The model can predict several simpler transitions instead of making one difficult leap. The extra text is useful only when the underlying model can exploit it; small models may merely produce longer, incorrect explanations.

Method and evidence: The authors changed prompting rather than model architecture. They supplied a small number of natural-language reasoning examples and evaluated several model families on arithmetic, commonsense, and symbolic benchmarks. With eight demonstrations, PaLM 540B reached strong GSM8K performance. A key result is scale dependence: the method's benefit emerged in larger models, so the technique cannot be assessed separately from model capacity and task design.

Agent relevance: Chain of thought made "reason before answering" a practical interface and inspired decomposition, reflection, and planning. An agent can use a reasoning pass to identify missing evidence or candidate actions. A prose chain, however, is not an execution plan: it has no stable IDs, dependencies, state, budget, or acceptance criteria. The runtime must convert useful intent into validated data before acting.

Limits and implementation: Generated reasoning can be plausible but unfaithful, and longer chains increase cost and may reinforce an early mistake. Treat model reasoning as a proposal, not proof. Ask for a structured plan or action contract, verify calculations and factual claims through tools, persist step state outside context, and use an independent evaluator. Compare success and token use against a direct-answer or shorter-reasoning baseline.`
    },
    {
      tags: ["RAG", "External Knowledge", "Retrieval"],
      overview:
        "Retrieval-Augmented Generation combines a language model with an external document index so generation can depend on retrieved evidence rather than parameters alone. It improves updateability and source access and provides the basic pattern later extended from document retrieval to agent memory and experience retrieval.",
      explanation: `Intuition: A parameter-only model answers like a closed-book student whose knowledge is difficult to update. RAG first retrieves relevant passages from an external corpus, then generates with those passages in context. The model gains an open-book path to current and inspectable information while retaining its generative ability. Retrieval is useful only when the search result is relevant and the model uses it correctly.

Method and evidence: The original work paired a dense passage retriever over Wikipedia with a pretrained sequence-to-sequence generator. RAG-Sequence conditions a whole answer on latent documents, while RAG-Token can vary document dependence across generated tokens. Retriever and generator are optimized by marginalizing latent documents. Experiments on knowledge-intensive tasks showed competitive open-domain question answering and more access to source material than a parameter-only baseline.

Agent relevance: RAG introduces the system pattern that knowledge and state can live outside model weights. Agent memory extends the retrieved object from encyclopedia passages to user preferences, prior tasks, failures, and skills. A complete loop must decide when to retrieve, reformulate a query, rank evidence, fit it into a context budget, cite it, and distinguish external facts from durable personal memory.

Limits and implementation: A confident generator can rationalize irrelevant or incorrect retrieval, and excessive retrieval adds noise and cost. External text may also contain prompt injection. Record source, time, access scope, and document identity; evaluate retrieval and answer support separately; constrain item and byte budgets; and require evidence for important claims. Retrieved content may inform a decision but must never grant tool permission.`
    },
    {
      tags: ["Tool Calling", "APIs", "Self-Supervision"],
      overview:
        "Toolformer explores how a language model can learn when to call an API, which API to choose, how to form arguments, and how to continue from the returned result. It moves tool use from a fixed hand-authored workflow toward a learned model policy, anticipating modern function-calling agents.",
      explanation: `Intuition: Placing a calculator or search endpoint next to a model does not teach the model when it is useful. Toolformer addresses four connected decisions: whether a tool is needed, which one fits, what arguments to provide, and how the result should affect the continuation. It teaches when to consult an instrument, not merely that an instrument exists.

Method and evidence: Researchers supplied a few examples for each API, sampled candidate calls inside a large text corpus, executed those calls, and inserted results back into text. A filtering rule retained examples only when the tool result improved language-model loss on following tokens. The experiments used calculator, search, question answering, translation, and calendar APIs and showed that self-supervised tool-call data could improve several downstream behaviors.

Agent relevance: The paper makes tool choice a learnable policy. Modern controllers express the choice as a structured action rather than special text, but descriptions, schemas, and return contracts still shape selection. The right metric is not whether an API returned 200; it is whether the invocation supplied evidence or changed environment state that improved task success at acceptable cost.

Limits and implementation: Toolformer assumes a relatively clean tool environment and optimizes language prediction. It does not manage filesystem authority, side effects, timeout, retry, rollback, or human approval. A host should parse a call proposal, validate the allowlist and schema, classify risk, request approval, enforce cancellation and output limits, and return the real result as an observation. Learned selection must remain separate from authorization.`
    },
    {
      title: "RLHF and Constitutional AI",
      tags: ["Alignment", "Preferences", "Safety Principles"],
      overview:
        "RLHF uses human comparisons to shape helpful and safe model behavior, while Constitutional AI introduces explicit principles, self-critique, and AI-generated preference feedback. Together they explain how pretrained generation can be steered, and why agent safety needs both learned behavior and enforceable host policy.",
      explanation: `Intuition: Pretraining teaches a model to continue text, not what an assistant should prefer or refuse. InstructGPT uses human demonstrations and rankings as coaching signals. Constitutional AI supplies an explicit set of principles, asks the model to critique and revise responses, and then derives AI preference labels from those principles. One relies more directly on human ranking; the other makes behavioral rules more explicit and scalable.

Method and evidence: A typical InstructGPT pipeline uses supervised fine-tuning, a reward model trained on pairwise rankings, and PPO constrained against a reference policy. Constitutional AI has a supervised self-critique/revision stage and an RLAIF stage using principle-guided AI comparisons. Both change the distribution of model output, but their supervision sources, algorithms, and evaluations differ and should not be collapsed into a single method.

Agent relevance: An agent may write files, run processes, browse networks, and retain user information, so conversational alignment is insufficient. Preference training can encourage useful defaults such as clarifying before acting, and a constitution can articulate consistent rules. Hard boundaries still belong in tool policy, data classification, approval, audit, and environment isolation because model-internal preferences can fail or be manipulated.

Limits and implementation: Preference datasets represent limited judgments, reward models can be exploited, and principles may conflict or remain ambiguous. Encode non-negotiable constraints in deterministic policy, leave softer trade-offs to model reasoning, and make rejection, approval, and escalation explicit states. Evaluate high-risk trajectories and blocked actions, not only whether final prose sounds polite or harmless.`
    }
  ],
  "agent-loop": [
    {
      tags: ["Survey", "Profiling", "Action"],
      overview:
        "This survey organizes LLM agents around profiling, memory, planning, and action. Its main value is a systems vocabulary: an agent is a collection of stateful modules around a goal, not a longer prompt or a single model response.",
      explanation: `Intuition: The survey turns a vague claim that an agent is "smart" into parts that can be inspected. A system has an identity and objective, retains selected experience, decides what to do next, and acts through an environment interface. Missing one part can reduce it to a one-shot chatbot or an uncontrolled script. The taxonomy makes failures easier to locate.

Method and evidence: The paper reviews work through profiling, memory, planning, and action. Profiling covers role, ability, and constraints; memory covers retention and retrieval; planning includes open-loop and feedback-based approaches; action includes text, tools, and embodied behavior. It also discusses capability acquisition, applications, evaluation, and safety. It is a field map, not a new controller algorithm.

Agent relevance: The categories map naturally to interfaces. A Controller owns the loop, a Planner proposes steps, Memory manages state, a Tool Layer executes, and an Evaluator determines completion. Once these boundaries exist, diagnosis becomes specific: the system may have retrieved the wrong fact, produced an invalid plan, failed a tool, or stopped on insufficient evidence.

Limits and implementation: A survey cannot choose the right storage, planner, or policy for a particular product, and the field changes rapidly. Start with the smallest loop that exposes inputs, actions, observations, budgets, errors, and stop reasons. Add a module only when a fixed task set shows that it improves success, cost, recovery, or safety; taxonomy compliance is not itself a product benefit.`
    },
    {
      tags: ["Survey", "Perception", "Action"],
      overview:
        "The paper describes LLM agents through brain, perception, and action and surveys assistants, software engineering, games, robotics, and social simulation. It reveals a common Observe-Decide-Act structure beneath agents that appear very different at the product surface.",
      explanation: `Intuition: The paper borrows a human analogy: a brain reasons, perception reads the environment, and action changes it. The analogy matters because an LLM is only part of the brain. Without trustworthy observations and constrained actuators, even a strong model can merely imagine that a task succeeded. Agency requires reading the environment again after action.

Method and evidence: The authors trace agent concepts and explain why LLMs can act as general foundations, then organize systems into brain, perception, and action. The brain covers interaction, knowledge, and reasoning; perception extends from text to visual and auditory signals; action includes tools and embodied control. The review surveys single-agent, multi-agent, human-agent, and social simulation applications and their open challenges.

Agent relevance: A browser agent observes DOM and pixels, an SRE agent observes logs and metrics, and Your Agent observes documents and tool results. Their controller loop can share a protocol while state representations, capabilities, policy, and success evidence differ. The framework helps define adapter boundaries: normalize observations, propose typed actions, execute, then read the actual state.

Limits and implementation: Brain, perception, and action are high-level categories; they do not solve multimodal grounding, rollback, or permission isolation. Every observation should retain source and time, every action should fit an explicit schema, and the system should verify post-action state rather than trusting a narrative. Add modality-specific evaluation because textual confidence cannot prove visual or physical success.`
    },
    {
      tags: ["Efficiency", "Memory", "Planning"],
      overview:
        "Toward Efficient Agents shifts attention from occasional task completion to stable completion at acceptable cost. It organizes efficiency work around memory, tool use, and planning and argues that quality must be considered together with tokens, latency, steps, calls, search depth, and communication.",
      explanation: `Intuition: An agent that succeeds after dozens of redundant model calls, repeated searches, or several agents restating the same context is not ready for routine use. Efficiency is not simply using fewer tokens. It is an effectiveness-first trade-off: reduce cost while preserving an acceptable outcome, or improve outcome under a fixed budget.

Method and evidence: The survey categorizes methods around Memory, Tool Use, and Planning and compares sources of cost such as context, retrieval, interaction steps, external calls, search, and multi-agent communication. It proposes system-level evaluation rather than treating model size as the only efficiency variable. Because it is a survey, individual empirical claims remain tied to the cited methods and benchmarks.

Agent relevance: The framework directly motivates runtime metrics. Token use, cache hits, latency, tool calls, retries, compactions, approvals, and task success should be recorded on the same trace. Improvements such as memory summaries or workflow routing can then be evaluated by fixed-quality cost or fixed-budget quality, rather than celebrated because one number decreased.

Limits and implementation: Efficiency results can shift with backbone model, provider cache, task distribution, and environment latency. A shorter trajectory may simply fail earlier. Establish a real task set and acceptance criteria first, then compare success and cost jointly. Persist enough detail to explain regressions, and do not optimize proxy metrics such as step count before verifying end-to-end outcomes.`
    }
  ],
  memory: [
    {
      tags: ["Memory Systems", "Lifecycle", "Benchmarking"],
      overview:
        "Memory in the LLM Era treats memory as a modular system rather than an ever-growing conversation. It distinguishes memory forms and operations and provides a framework for asking what is written, how it changes, when it is retrieved, and how it affects downstream agent behavior.",
      explanation: `Intuition: A long transcript is not the same as memory. Useful memory is selective: it preserves stable facts or experience, retrieves them for the right task, revises outdated beliefs, and forgets what should no longer influence behavior. The paper gives vocabulary for comparing mechanisms that otherwise all claim to provide "long-term memory."

Method and evidence: The survey organizes memory by representation, operation, source, and use, spanning short-term context, external stores, parameterized knowledge, and agent experience. It reviews creation, organization, retrieval, update, and forgetting and relates them to evaluation. The framework helps compare systems, but reported rankings still depend on backbone models, tasks, and retrieval budgets.

Agent relevance: A runtime can translate the taxonomy into schemas and APIs. Records need scope, provenance, confidence, lifecycle status, and timestamps; write behavior needs add, update, merge, archive, delete, and retrieve; retrieval needs item and byte budgets. Session history, RAG evidence, user facts, and reusable procedures should remain distinct even when they share storage technology.

Limits and implementation: No single memory method dominates every task, and adding a store can reduce quality through stale or irrelevant recall. Begin with a transparent lifecycle store and log writes, candidates, retrieved evidence, conflicts, and outcome. Evaluate whether memory changes task success, not only retrieval similarity. Add embeddings or graph structure only when real failure analysis demonstrates the need.`
    },
    {
      tags: ["Dynamic Memory", "Update", "Linking"],
      overview:
        "A-MEM models memory as an evolving network whose entries can gain context, links, and revisions when new information arrives. It addresses duplication, contradiction, and staleness in append-only memory and argues that organization is part of the agent policy.",
      explanation: `Intuition: Append-only memory resembles throwing notes into a drawer. Writing is easy, but related notes remain disconnected and old claims survive after circumstances change. A-MEM draws on a card-index idea: a new memory receives context and tags, finds related old memories, and can change how the collection is organized.

Method and evidence: New content is transformed into a structured note, relevant prior notes are retrieved, and the system creates meaningful links. Memory evolution lets new evidence update contextual representations and attributes of existing notes. The paper compares this adaptive organization across several models and agent tasks, aiming to show that evolving structure helps more than fixed insert-and-retrieve operations.

Agent relevance: The work suggests APIs for ADD, LINK, UPDATE, MERGE, ARCHIVE, and RETRIEVE, along with a versioned reason for every change. A project decision can supersede an old status while preserving the original user statement and history. Retrieval quality depends on organization after write, not merely on the embedding model used at query time.

Limits and implementation: Letting an LLM rewrite old memory can propagate one mistaken inference through the network and adds model cost. The method does not automatically solve tenant permission or poisoned input. Keep raw evidence separate from derived summaries, use confidence and confirmation for high-value changes, retain versions and soft deletion, and replay updates against fixed tasks before enabling automatic evolution.`
    },
    {
      tags: ["Hierarchical Memory", "Long-Horizon Tasks", "Compaction"],
      overview:
        "HiAgent addresses context growth in long-horizon tasks with hierarchical working memory. It retains detail for the active subgoal, compresses completed subgoals, and can retrieve older trajectories, reducing context while preserving enough state for continued action.",
      explanation: `Intuition: A long task produces an expanding action-observation transcript. Replaying every detail is like forcing a team to listen to every prior meeting before taking the next step. HiAgent treats subgoals as memory blocks: keep the current block in detail, replace completed blocks with concise outcomes, and reopen an old trajectory only when it becomes relevant.

Method and evidence: The agent generates subgoals, acts and accumulates a trajectory, detects subgoal completion, and uses observation summarization to replace the completed block. Trajectory retrieval can later restore detail. On five AgentBoard tasks exceeding twenty steps, the paper reports higher average success, fewer steps, and lower context-token use, demonstrating that planning boundaries can also be memory boundaries.

Agent relevance: Planning and memory should be designed together. Your Agent keeps recent observations, compresses completed phases, and retains complete Session and JSONL history outside the prompt. A subgoal or user-turn boundary provides a safer cut point than truncating arbitrary tokens. Summary provenance permits later inspection when a compacted view appears incomplete.

Limits and implementation: A summary may discard detail whose relevance appears only later, and an incorrect subgoal-completion decision can compress too early. Preserve original messages and traces, store coverage metadata, and allow a fallback to raw evidence. Evaluate success, tokens, summary cost, and context-recovery frequency together; compression that saves tokens but increases retries may not be efficient.`
    },
    {
      tags: ["Security", "Memory Poisoning", "RAG"],
      overview:
        "AgentPoison demonstrates that a small number of malicious entries in long-term memory or a RAG store can influence future agent decisions under a trigger. It reframes memory as a persistent attack surface and motivates provenance, write governance, isolation, and adversarial evaluation.",
      explanation: `Intuition: A normal prompt injection attempts to influence the current interaction. Memory poisoning plants a misleading experience that appears harmless until a future query retrieves it. A trigger can make the malicious item unusually similar to the query, placing it in context and steering planning or action while ordinary behavior remains mostly normal.

Method and evidence: The attack targets agents backed by long-term memory or retrieval without retraining the base model. It optimizes triggers and poisoned examples in representation space so triggered queries retrieve the attack content while benign performance stays relatively stable. Experiments across agents, stores, and models evaluate attack success, transfer, robustness, and stealth.

Agent relevance: Web pages, emails, shared documents, and tool output can create risk that survives the current turn if automatically written to trusted memory. A memory record needs source, tenant, scope, author, time, and lifecycle. Retrieval may provide evidence but must never authorize file writes, commands, or production API calls; those actions require current policy checks.

Limits and implementation: Not every trigger transfers to every embedding and retriever, but the attack invalidates the assumption that a vector store is inherently trustworthy. Separate external candidate memory from confirmed user facts, hash and audit writes, support revocation, down-rank or isolate untrusted sources, and add poisoning scenarios to continuous evaluation of the combined Memory and Tool layers.`
    }
  ],
  "tool-use": [
    {
      tags: ["Tool Calling", "APIs", "Policy"],
      overview:
        "Viewed from an agent system, Toolformer establishes that tool selection and argument construction can be learned. The engineering consequence is a strict separation: the model supplies a policy proposal, while a registry and policy engine own execution, side effects, evidence, and recovery.",
      explanation: `Intuition: Tool use is a decision under uncertainty. The model should call a capability when the expected value of better evidence or a correct state change exceeds the cost and risk. Toolformer shows that this decision can be shaped from examples and loss, but a real environment introduces failure, permission, and irreversible effects that the language-model objective does not represent.

Method and evidence: The paper generates candidate API calls, executes them, and retains calls that improve prediction of later tokens. This self-supervised filtering teaches invocation timing and argument patterns for several APIs. The result supports learned selection, not a universal tool protocol. Modern agents commonly replace inline call syntax with typed JSON or provider function events.

Agent relevance: Tool descriptions, argument schemas, observations, and examples form the action vocabulary of a controller. The host should also record why a call was made, how long it took, whether it failed, how much output entered context, and whether it improved the task. An efficient agent may call more tools when they replace expensive uncertain reasoning.

Limits and implementation: Training loss does not express user authorization, filesystem scope, network safety, idempotency, or rollback. Keep an explicit registry, validate unknown and required arguments, enforce read/write/dangerous risk, approval, timeout, cancellation, and output limits, and read the real environment after action. Never interpret a model-generated tool name as permission.`
    },
    {
      title: "Toward Efficient Agents: Tool Use",
      tags: ["Tool Selection", "Cost", "Tool-Integrated Reasoning"],
      overview:
        "The tool-use portion of Toward Efficient Agents studies selection, invocation, and tool-integrated reasoning as a quality-cost trade-off. Efficient behavior means using a capability when its expected contribution exceeds token, latency, monetary, and risk cost, not minimizing calls in isolation.",
      explanation: `Intuition: A search call can save several speculative reasoning turns, while an unnecessary call can add latency, noise, and an attack surface. The useful question is not "did the agent call fewer tools?" but "did the selected calls improve accepted outcomes under a stated budget?" Tool choice and reasoning must be evaluated as one policy.

Method and evidence: The survey groups methods for choosing capabilities, generating valid calls, and incorporating results into subsequent reasoning. It compares efficiency dimensions such as model calls, tool calls, tokens, latency, and task quality. As a survey, it aggregates methods with different environments and cost assumptions, so conclusions require careful normalization before direct ranking.

Agent relevance: A runtime should log candidate tool, actual invocation, schema or permission rejection, duration, bounded output, and downstream acceptance. Routing can use cheaper models for simple selection and stronger models for difficult synthesis, while deterministic code handles safety. Prompt cache and context compaction also influence the real cost of tool-integrated reasoning.

Limits and implementation: A low call count may conceal missing evidence, and a benchmark tool may be faster or safer than its production counterpart. Define fixed tasks and acceptance criteria, measure quality and cost together, classify retries and no-op calls, and compare against no-tool and fixed-workflow baselines. Keep policy decisions reproducible enough to diagnose why a cost optimization changed success.`
    },
    {
      tags: ["Multi-Agent", "Conversation", "Human Input"],
      overview:
        "AutoGen provides a conversation-centered abstraction for composing LLM agents, tools, code execution, and human participation. Its value is an interaction protocol that can organize heterogeneous actors, while also exposing the cost and failure modes of message-based orchestration.",
      explanation: `Intuition: Complex work may involve a researcher, executor, reviewer, and person. AutoGen treats these participants as conversable agents that exchange messages and can invoke tools or request input. This is flexible, but adding another speaker does not automatically add intelligence; it adds a stateful communication channel that must have a purpose and a stop rule.

Method and evidence: The framework defines configurable conversable agents, supports LLM-backed and tool-backed behavior, and demonstrates workflows such as code generation, execution, and critique. The associated paper evaluates applications and developer flexibility rather than establishing one optimal multi-agent topology. Message protocols are the central abstraction.

Agent relevance: AutoGen suggests useful boundaries for adapters, tool executors, and human-in-the-loop transitions. Structured messages and explicit ownership make interaction recoverable. A single-agent runtime can borrow these protocol ideas without creating many personas; multiple roles become valuable when subproblems can run independently, require permission isolation, or benefit from separate expertise.

Limits and implementation: Conversational orchestration can produce duplicate work, loops, growing context, and unclear responsibility. Define typed artifacts, role authority, message budgets, termination, and evaluation before adding participants. Record communication cost and merge evidence rather than opinions. Prefer one controller until a measured bottleneck demonstrates that another role improves outcome or latency.`
    },
    {
      tags: ["Multi-Agent", "SOP", "Software Engineering"],
      overview:
        "MetaGPT maps software-team roles and standard operating procedures into a multi-agent workflow with structured intermediate artifacts. It argues that explicit process and deliverables can reduce ambiguity, while introducing coordination and rigidity that must be measured.",
      explanation: `Intuition: Asking several agents to chat freely resembles a meeting without an agenda. MetaGPT assigns roles and a standard process: requirements become specifications, designs, tasks, code, and reviews. Structured artifacts carry information between stages, making each handoff more concrete than unbounded conversation.

Method and evidence: The system encodes software roles, SOPs, and communication through shared structured outputs. Experiments focus on generating software projects and compare quality or executability against other agent approaches. The contribution is primarily process representation and orchestration rather than a new base model or universal planning algorithm.

Agent relevance: A long task can benefit from typed deliverables, explicit acceptance, and role-specific capabilities even inside a single runtime. Plans can assign a researcher and reviewer to independent steps, persist their outputs, and verify the artifact before the next dependency. The underlying principle is information ownership, not imitation of a company hierarchy.

Limits and implementation: A fixed SOP can become expensive or inappropriate for small and novel tasks, and generated artifacts may appear complete without being correct. Start with the shortest validated workflow, skip unnecessary roles, test actual code or environment state, and measure communication. Add a role only when specialization, parallelism, or privilege separation provides a repeatable benefit.`
    },
    {
      tags: ["Workflow Search", "MCTS", "Optimization"],
      overview:
        "AFlow represents agent workflows as executable structures and searches over their composition using task feedback. It suggests that orchestration itself can be optimized, but only when evaluation is stable enough that search does not overfit or exploit a weak judge.",
      explanation: `Intuition: After a workflow is executable, one can ask whether a different ordering, branch, or combination of model calls works better. AFlow treats the workflow as a program that can be modified and evaluated, rather than a hand-written permanent recipe. The search is useful only when the score reflects the real task.

Method and evidence: The method composes LLM operators and control flow and uses Monte Carlo Tree Search to explore workflow candidates based on benchmark feedback. Results across reasoning tasks indicate that optimized workflows can outperform fixed baselines. The evaluation remains tied to the tasks, models, search budget, and score functions used in the experiments.

Agent relevance: A production-shaped runtime already has the ingredients needed before workflow search: structured plans, traces, deterministic checks, cost metrics, and versioned configurations. It can compare whether retrieval precedes planning, whether a reviewer step helps, or which model handles each role. Candidate workflows should be treated like code releases.

Limits and implementation: Search can consume substantial model calls and exploit a weak or narrow evaluator. It may produce brittle structures that fail outside the benchmark. Freeze a representative task set, combine outcome and cost, maintain holdout tasks, version every candidate, and require regression and rollback. Do not optimize an orchestration layer that is not yet reliably observable.`
    }
  ],
  planning: [
    {
      tags: ["Planning Survey", "Monitoring", "Replanning"],
      overview:
        "This survey organizes LLM-agent planning around task decomposition, plan generation, action selection, feedback, monitoring, and replanning. It clarifies that producing a plausible list of steps is only one part of planning; execution state and recovery complete the system.",
      explanation: `Intuition: A plan is useful when it changes what the system can do next and reveals whether progress occurred. A paragraph that says "research, write, review" sounds organized but cannot be resumed or verified. Agent planning must connect the goal to typed actions, dependencies, evidence, failure recovery, and a stop decision.

Method and evidence: The survey categorizes planning approaches, including decomposition, external planners, feedback-driven refinement, search, reflection, and multi-agent coordination. It compares evaluation settings and discusses limits of LLM planning. Because it synthesizes diverse tasks, no single benchmark result establishes a universally best method.

Agent relevance: The taxonomy maps to a persisted plan schema: step ID, description, dependency, tool, role, risk, budget, success criteria, status, attempts, and evidence. A scheduler can dispatch ready nodes, a verifier can check artifacts, and a controller can replan after an observation. The plan survives context compression and process restart.

Limits and implementation: Models may create invalid dependencies, impossible actions, or plans optimized for narrative coherence. Validate IDs, cycles, available tools, and acceptance checks before execution. Bound plan size, persist each transition, make irreversible steps require approval, and compare planning overhead against a direct reactive baseline. Simple tasks should be allowed to skip a heavyweight DAG.`
    },
    {
      title: "Chain-of-Thought from a Planning Perspective",
      tags: ["Reasoning", "Plan Proposal", "Structured Execution"],
      overview:
        "From a planning perspective, Chain-of-Thought is useful for proposing decomposition and identifying missing information, but its free-form reasoning lacks stable state, dependencies, and acceptance. Agent engineering must translate useful reasoning into an executable contract.",
      explanation: `Intuition: A reasoning chain is a scratchpad; a plan is a shared ledger. The scratchpad may contain a valuable strategy, but it can skip a dependency or rewrite its own history. The ledger must identify what is pending, running, blocked, accepted, or failed. Confusing the two makes long-task recovery dependent on the model's latest prose.

Method and evidence: Chain-of-Thought prompting elicits intermediate natural-language steps and improves selected reasoning benchmarks at sufficient model scale. It was not designed as a persistent scheduler and does not experimentally validate dependency recovery, concurrency, idempotency, or human acceptance. Those are engineering extensions motivated by the same desire to make intermediate process explicit.

Agent relevance: A planner can use model reasoning to propose candidate steps, then serialize them into a validated DAG. The controller supplies current state and observations; the scheduler chooses ready work; verifiers and people accept evidence. This preserves model flexibility while moving execution semantics into data that can be inspected and resumed.

Limits and implementation: Longer reasoning may increase confidence without correctness and may expose sensitive internal context if logged indiscriminately. Request concise structured rationale where needed, store decisions and evidence rather than unrestricted hidden reasoning, and validate every executable field. Test interruption and restart so the system proves that planning state, not the prompt, controls progress.`
    },
    {
      tags: ["Multi-Agent", "Scaling", "Communication Cost"],
      overview:
        "Research on scaling LLM multi-agent collaboration finds potential gains from diverse roles and parallel work, but also strong dependence on communication structure, task decomposition, and coordination cost. More agents are an architectural trade-off rather than a default scaling law.",
      explanation: `Intuition: Several agents can examine independent parts of a task at once or review one another, but they can also repeat the same work and spread one error through many messages. Collaboration helps when subproblems are separable and outputs can be verified. Otherwise, each participant adds context, latency, and another policy boundary.

Method and evidence: Multi-agent scaling studies vary agent count, communication topology, role diversity, and task setting to measure quality and cost. Results show that gains are not monotonic: coordination and information aggregation can become bottlenecks. Conclusions depend on the base model and environment, so agent count alone is not a meaningful comparison.

Agent relevance: A persisted plan DAG makes parallelism explicit. Independent ready steps can run under researcher or reviewer roles and merge through a verifier. Permission isolation can justify separate agents even without quality gain. Runtime metrics should attribute model calls, tokens, tool actions, and failures by role so coordination overhead remains visible.

Limits and implementation: Multi-agent evaluation may reward discussion length or benchmark-specific voting. Begin with one controller and add a role for a measured reason: independent parallel work, specialized context, adversarial review, or privilege separation. Define output schemas and merge rules, cap communication, cancel sibling work after failure, and compare against a stronger single-agent baseline.`
    }
  ],
  "evaluation-safety": [
    {
      tags: ["Mobile Agents", "Real Environments", "Task Success"],
      overview:
        "SPA-Bench evaluates smartphone agents through tasks that require actual interaction and state change. It illustrates why answer quality is insufficient for action systems: success must be read from the environment after the agent acts.",
      explanation: `Intuition: A mobile agent can confidently say it sent a message while the application remains on the compose screen. SPA-Bench focuses on whether the device reached the requested state, not whether the final narration sounds correct. This distinction applies to every tool-using agent: an action request and a successful outcome are different events.

Method and evidence: The benchmark defines smartphone tasks, environments, action interfaces, and evaluation procedures that exercise navigation and state changes. It compares agents under real or realistic interaction constraints rather than static question answering. Specific scores are tied to device setup, application versions, and task definitions, which are part of the benchmark contract.

Agent relevance: A GUI controller should record observations before and after each action, preserve screenshots or structured state, and let an external verifier determine success. The same pattern maps to Your Agent citations, coding-agent tests, and operations-agent metrics. Environment evidence should enter the trace and stop decision.

Limits and implementation: Mobile applications change, nondeterministic UI delays complicate scoring, and a benchmark may not represent private workflows. Version the environment, reset state between runs, distinguish action failure from verification failure, and include latency and human intervention. Never use the model's own completion sentence as the only success signal.`
    },
    {
      tags: ["Cybersecurity", "Capability Evaluation", "Risk"],
      overview:
        "CyBench evaluates language-model agents on executable cybersecurity tasks. It provides evidence about both capability and misuse risk and demonstrates the need for sandboxing, permission control, audit, and careful release decisions when tools have high impact.",
      explanation: `Intuition: Cybersecurity tasks expose the gap between discussing a solution and performing a multi-step technical procedure. A model must inspect artifacts, use tools, adapt after failure, and obtain a verifiable result. The same ability that helps defensive analysis can enable misuse, so capability evaluation and risk evaluation cannot be separated.

Method and evidence: The benchmark contains professional cybersecurity tasks with environments, subtasks, and answer or state checks. It evaluates models and agents over long interactions and analyzes performance by difficulty and enabling factors. Results represent the tested setup rather than unrestricted real-world competence, but they reveal where tool access changes the risk profile.

Agent relevance: High-impact tools need explicit risk levels, environment isolation, narrow credentials, cancellation, and a complete trace. Evaluators should inspect actual artifacts or flags rather than narrative confidence. Deployment policy can use task class and requested capability to route to denial, a sandbox, or human approval.

Limits and implementation: Benchmarks may expose knowledge that does not transfer to changing targets, and success can depend on scaffolding. Do not test dangerous behavior against systems without authorization. Build resettable environments, redact secrets, separate defensive from offensive scopes, and treat human approval as one layer rather than a substitute for sandboxing and least privilege.`
    },
    {
      title: "AgentPoison from a Safety Perspective",
      tags: ["Memory Security", "Persistent Attacks", "Provenance"],
      overview:
        "From a safety perspective, AgentPoison shows that untrusted retrieval can create delayed, cross-session influence. Safety policy must therefore govern memory writes and reads as well as immediate prompts and tool calls.",
      explanation: `Intuition: A malicious web page may not need to make the current agent act. If its content is stored as trusted experience, it can influence a later task when no one remembers the original source. Persistent state extends the lifetime of an attack and makes clean-up and attribution harder.

Method and evidence: AgentPoison constructs a retrieval-oriented backdoor by inserting a small set of crafted memories and optimizing a trigger so they are recalled under targeted queries. Experiments measure triggered attack success while trying to preserve ordinary task behavior. This stealth property is precisely what makes memory governance different from an obvious current-turn injection.

Agent relevance: Memory schemas need provenance, tenant and scope, write actor, confidence, lifecycle, and revocation. Automatic write paths from tools should produce low-trust candidates, not confirmed facts. On retrieval, instructions embedded in evidence must remain data. Current tool policy and approval must be re-evaluated for every side effect.

Limits and implementation: Detection techniques may be specific to an embedding or trigger class, and no filter guarantees complete protection. Use layered controls: restrict write sources, isolate scopes, log retrieval, detect anomalous similarity, require confirmation for consequential memory, and support deletion and replay. Include poisoned and conflicting records in evaluation before shipping a memory change.`
    }
  ],
  "rl-optimization": [
    {
      tags: ["Preference Optimization", "Pairwise Data", "Alignment"],
      overview:
        "Direct Preference Optimization trains a policy directly from preferred and rejected responses without fitting a separate reward model and running PPO. For agents, the same pairwise idea can compare complete trajectories, including evidence use, tool choices, cost, and permission behavior.",
      explanation: `Intuition: Preference data says that one behavior is better than another without requiring an absolute numeric score. DPO turns those pairs into a direct classification-like objective that increases the relative likelihood of the preferred response while remaining anchored to a reference policy. This simplifies an influential RLHF pipeline.

Method and evidence: DPO derives a relationship between an optimal KL-regularized policy and its implicit reward, then optimizes a closed-form preference objective on chosen/rejected pairs. Experiments compare quality and training stability with PPO-based preference optimization on language tasks. It is still offline learning from curated comparisons and depends heavily on pair quality and distribution.

Agent relevance: An agent preference pair can compare trajectories rather than only final prose: one agent verifies a source, avoids an irrelevant tool, asks before a write, and finishes cheaply; another does not. That requires structured logs with goals, actions, observations, policy decisions, outcome, and cost. Pair construction can target a specific behavior while holding task difficulty constant.

Limits and implementation: DPO can inherit annotator bias, overfit pair style, and optimize preferences that do not reflect environment success. First build a stable task set and external evaluator. Keep a holdout, inspect regressions by action type, and never use preference training to replace deterministic permission checks. A more agreeable model is not automatically a safer executor.`
    },
    {
      tags: ["Reasoning RL", "Verifiable Rewards", "Long Reasoning"],
      overview:
        "DeepSeek-R1 demonstrates that large-scale reinforcement learning can elicit extended reasoning, self-checking, and strategy changes, while the full training pipeline also uses cold-start data, supervised stages, rejection sampling, and distillation. Its agent relevance comes from verifiable outcome rewards.",
      explanation: `Intuition: Instead of supplying every reasoning step, a model can repeatedly solve tasks with checkable answers and receive feedback about the outcome. Over many trials it may develop reflection and strategy-switching behavior. The pure-RL R1-Zero experiment is important, but the released DeepSeek-R1 pipeline is not accurately described as using no supervised data at all.

Method and evidence: R1-Zero applies large-scale RL to a base model and observes emergent reasoning patterns along with readability and language-mixing problems. Full R1 adds cold-start examples, reasoning RL, rejection-sampling supervised fine-tuning, and later stages for broader ability. The work also distills strong-model reasoning data into smaller models and evaluates across mathematical, coding, and knowledge tasks.

Agent relevance: Tests, exact answers, browser state, and game scores can provide stronger outcome rewards than subjective prose. Complete trajectories let optimization shape when to inspect evidence, retry a tool, or change approach. Logs must preserve observations and external results; a final answer alone cannot reveal which behavior produced the reward.

Limits and implementation: Verifiable tasks cover only part of real agent work, training is costly, and outcome rewards can encourage long reasoning, unsafe shortcuts, or reward hacking. Collect stable traces and deterministic evaluation first, combine outcome with cost and policy penalties, train in a resettable sandbox, and require offline and shadow regression before any policy receives broader authority.`
    },
    {
      tags: ["Search", "Tool RL", "Evidence"],
      overview:
        "Search-R1 uses reinforcement learning to optimize when a reasoning model searches, how it forms queries, and how it uses returned evidence. The method treats search as part of a multi-step policy rather than a fixed retrieve-once prefix.",
      explanation: `Intuition: Connecting a search engine does not teach a model when information is missing, which query will close the gap, or when enough evidence has been collected. Search-R1 lets the model interleave reasoning and multiple search calls, then uses final question-answer correctness to shape the full behavior.

Method and evidence: Search calls are embedded in the reasoning trajectory. Retrieved tokens are environment output rather than model actions, so they are masked from policy-gradient calculation. Training uses an outcome correctness reward. The paper reports improvements for smaller Qwen2.5 models across seven question-answering datasets and studies algorithms, model choices, and answer length.

Agent relevance: Research agents should be able to reformulate queries, continue when evidence conflicts, cite the retrieved source, and stop under a budget. Traces must distinguish model tokens from observations and store query, source, result, and final support. An evaluator should measure answer correctness, evidence coverage, source quality, calls, and latency together.

Limits and implementation: Correct-answer reward can tolerate poor sources or accidental retrieval, and training distributions may teach templates rather than general search judgment. Real web content changes and carries injection risk. Restrict domains and call budgets, isolate page instructions, retain source snapshots, and evaluate time-sensitive and adversarial tasks before trusting a learned search policy.`
    },
    {
      tags: ["Multi-Turn RL", "Long-Horizon", "Trajectories"],
      overview:
        "AgentGym-RL provides a framework for training agents through multi-turn interaction and long-horizon decisions. It moves the learning unit from an isolated response to a complete environment trajectory, where early actions influence later observations and rewards.",
      explanation: `Intuition: Single-turn training resembles answering one question. A long-horizon agent changes the environment, sees a new state, and may discover the consequence of an early mistake many steps later. Training must therefore manage sparse reward, exploration, environment reset, and credit across the whole sequence.

Method and evidence: AgentGym-RL uses a modular architecture to connect several environments and RL algorithms. ScalingInter-RL begins with shorter interactions to exploit existing ability and gradually increases the horizon to encourage exploration as training stabilizes. The curriculum attempts to avoid losing useful reward when the initial action space is too large.

Agent relevance: A runtime trace needs observation, action, tool arguments, environment result, reward, and stop reason. Your Agent already stores many of these fields, but RL additionally requires resettable tasks, stable versioned reward, train/evaluation isolation, and enough repeated interaction to estimate policy change. Production user work is not an exploration environment.

Limits and implementation: Long horizons create sparse feedback and high sampling cost, and framework gains depend on the selected tasks. Build sandbox tasks first, increase budgets gradually, classify intermediate failures, and compare against supervised or fixed-policy baselines. Keep unvalidated policies in offline or shadow execution until outcome, cost, and safety regressions pass.`
    },
    {
      tags: ["Skill Library", "Reuse", "Recursive Evolution"],
      overview:
        "SkillRL distills successful experience into reusable skills that can be retrieved, composed, and evolved with policy learning. It aims to reduce repeated exploration by turning validated behavioral patterns into explicit assets rather than replaying full trajectories.",
      explanation: `Intuition: An agent that rereads all previous runs still behaves like a beginner searching a diary. A skill is a compact method such as "verify dependencies before running the target test" with conditions and expected evidence. A future task retrieves a relevant skill, adapts it, and contributes new experience to its evolution.

Method and evidence: The framework distills raw trajectories into a hierarchical SkillBank with general and task-specific knowledge, retrieves a limited relevant subset, and recursively evolves the skill store with the RL policy. Experiments in ALFWorld, WebShop, and search-augmented tasks report gains over baselines while reducing the token redundancy of raw trajectory reuse.

Agent relevance: Skills sit between memory and policy. They need triggers, inputs, outputs, tool dependencies, failure handling, version, provenance, and acceptance statistics. A Your Agent procedure for paper comparison or citation checking should be promoted only after repeated accepted runs and should remain subject to the current plan and tool policy.

Limits and implementation: A wrong or over-specific skill can replicate old bias quickly, and automatic evolution can make behavior hard to reproduce. Require minimum evidence, applicability scope, version history, rollback, and retirement. Evaluate retrieval and downstream outcome through A/B tests and ablation. A skill library should shorten reliable behavior, not become another ever-growing prompt.`
    }
  ],
  engineering: [
    {
      title: "AutoGen, MetaGPT, and AFlow as an Engineering Sequence",
      tags: ["Architecture", "Workflow", "SOP"],
      overview:
        "This combined review uses AutoGen for interaction protocols, MetaGPT for structured procedures and artifacts, and AFlow for workflow search. Together they suggest an engineering order: make execution explicit, make it measurable, and only then optimize orchestration.",
      explanation: `Intuition: The three projects address different levels. AutoGen asks how participants exchange messages and tools. MetaGPT asks how roles and standard artifacts reduce coordination ambiguity. AFlow asks whether an already executable workflow can be improved through search. Reading them as a sequence is more useful than copying a large multi-agent demo.

Method and evidence: AutoGen defines conversable actors, MetaGPT encodes software roles and SOP artifacts, and AFlow represents LLM operators and searches workflow structures with MCTS. Their tasks, metrics, and contributions differ, so experimental improvements cannot be added together. The common thread is that behavior becomes an explicit, inspectable orchestration object.

Agent relevance: A personal agent can remain a single controller while implementing clear Provider, Planner, Tool, Memory, Evaluator, and Logger boundaries. Messages, plans, artifacts, and outcomes should be structured and persisted. Add roles only for independent parallel work, specialization, review, or permission isolation. Optimize workflow order only after evaluation is repeatable.

Limits and implementation: Multiple agents add communication cost, SOPs can be rigid, and workflow search can overfit a weak evaluator. Use the progression loop first, observability second, optimization last. Version every role and workflow change, cap budgets, maintain a holdout task set, and require a measurable improvement in outcome, latency, cost, or safety without unacceptable regression.`
    },
    {
      title: "Toward Efficient Agents: Engineering Perspective",
      tags: ["Reference Runtime", "Observability", "Productization"],
      overview:
        "This project maps the effectiveness-first principles of Toward Efficient Agents into a Go runtime: provider routing, controlled tools, durable sessions/goals/plans/memory, layered context compression, evaluation, metrics, and real user surfaces in one regression-tested loop.",
      explanation: `Intuition: A reference agent is valuable when it demonstrates accepted outcomes under visible cost and policy, not when it merely contains components with fashionable names. Your Agent keeps a deterministic mode and a real provider behind the same contracts, so the surrounding state machine can be tested without confusing model randomness with runtime correctness.

Method and evidence: This is a maintainer engineering mapping, not a new algorithm claimed by the survey. The current runtime includes SSE and model fallback, structured plan and action parsing, DAG validation and persistence, controlled file/shell/web/plugin/MCP tools, scoped SQLite memory, independent Goal state, transactional canonical Session turns with native reasoning/tool blocks, L1/L2/L3 compaction, deterministic evaluation, and redacted traces with efficiency metrics.

Agent relevance: CLI, readline, TUI, Web UI, HTTP/SSE, and Feishu reuse one core. Build, E2E, browser, open-source, and cross-platform release checks exercise product surfaces rather than only unit functions. The state split and metrics create a foundation for domain-specific tools, evaluation sets, and later skill or preference optimization without binding the system to one model vendor.

Limits and implementation: The runtime is not yet a complete research product or hardened multi-tenant service. PDF extraction, citation entailment, browser automation, memory conflict resolution, tenant isolation, encrypted Feishu callbacks, and production sandboxing need additional work. The next milestone is fixed real-task evaluation and stronger evidence tools; RL should wait for stable rewards and a high-quality trace corpus.`
    }
  ]
};

export const modules = sourceModules.map((module) => {
  const translatedModule = moduleTranslations[module.id];
  const translatedPapers = paperTranslations[module.id];
  if (!translatedModule || translatedPapers.length !== module.papers.length) {
    throw new Error(`Missing English translation for module ${module.id}`);
  }
  return {
    ...module,
    ...translatedModule,
    papers: module.papers.map((paper, index) => ({
      ...paper,
      ...translatedPapers[index]
    }))
  };
});

export const architecture = [
  { name: "Controller", detail: "Chooses whether to answer, plan, call a tool, request clarification, continue, pause, or stop." },
  { name: "Planner", detail: "Produces structured steps with dependencies, roles, tools, evidence, acceptance, and recoverable state." },
  { name: "Tool Layer", detail: "Owns schemas, risk, approval, timeout, cancellation, output bounds, host policy, and observations." },
  { name: "Session and Goal", detail: "Persist complete conversation and independent long-running objective state across turns and restarts." },
  { name: "Memory Layer", detail: "Manages selected cross-task facts through scopes, provenance, confidence, lifecycle, and retrieval budgets." },
  { name: "Evaluator", detail: "Checks real completion evidence, permissions, cost, and whether a retry or human acceptance is required." },
  { name: "Logger and Metrics", detail: "Store replayable events, token and cache use, latency, tool behavior, approvals, and task outcome." },
  { name: "Skill Library", detail: "Promotes repeatedly validated procedures into versioned reusable skills with scope and rollback." }
];

export const metrics = [
  "task_success_rate",
  "citation_accuracy",
  "input_tokens / output_tokens / cache_tokens",
  "latency",
  "tool_call_count / tool_failure_count",
  "retry_count / context_compactions",
  "memory_hit_rate",
  "human_intervention_count",
  "unsafe_action_blocked_count",
  "failure_reason"
];
