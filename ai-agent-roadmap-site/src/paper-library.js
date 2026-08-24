const paperAssets = Object.freeze({
  transformer: {
    title: "Attention Is All You Need",
    file: "attention-is-all-you-need.pdf",
    sources: [
      "https://proceedings.neurips.cc/paper/2017/file/3f5ee243547dee91fbd053c1c4a845aa-Paper.pdf",
      "https://arxiv.org/pdf/1706.03762"
    ]
  },
  gpt3: {
    title: "Language Models are Few-Shot Learners",
    file: "language-models-are-few-shot-learners.pdf",
    sources: [
      "https://proceedings.neurips.cc/paper_files/paper/2020/file/1457c0d6bfcb4967418bfb8ac142f64a-Paper.pdf",
      "https://arxiv.org/pdf/2005.14165"
    ]
  },
  cot: {
    title: "Chain-of-Thought Prompting Elicits Reasoning in Large Language Models",
    file: "chain-of-thought-prompting.pdf",
    sources: [
      "https://proceedings.neurips.cc/paper_files/paper/2022/file/9d5609613524ecf4f15af0f7b31abca4-Paper-Conference.pdf",
      "https://arxiv.org/pdf/2201.11903"
    ]
  },
  rag: {
    title: "Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks",
    file: "retrieval-augmented-generation.pdf",
    sources: [
      "https://proceedings.neurips.cc/paper/2020/file/6b493230205f780e1bc26945df7481e5-Paper.pdf",
      "https://arxiv.org/pdf/2005.11401"
    ]
  },
  toolformer: {
    title: "Toolformer: Language Models Can Teach Themselves to Use Tools",
    file: "toolformer.pdf",
    sources: [
      "https://proceedings.neurips.cc/paper_files/paper/2023/file/d842425e4bf79ba039352da0f658a906-Paper-Conference.pdf",
      "https://arxiv.org/pdf/2302.04761"
    ]
  },
  instructgpt: {
    title: "Training Language Models to Follow Instructions with Human Feedback",
    file: "instructgpt-rlhf.pdf",
    sources: ["https://arxiv.org/pdf/2203.02155"]
  },
  constitutionalAi: {
    title: "Constitutional AI",
    file: "constitutional-ai.pdf",
    sources: ["https://arxiv.org/pdf/2212.08073"]
  },
  autonomousAgentsSurvey: {
    title: "A Survey on Large Language Model based Autonomous Agents",
    file: "llm-autonomous-agents-survey.pdf",
    sources: [
      "https://arxiv.org/pdf/2308.11432",
      "https://link.springer.com/content/pdf/10.1007/s11704-024-40231-1.pdf"
    ]
  },
  riseOfAgents: {
    title: "The Rise and Potential of Large Language Model Based Agents",
    file: "rise-and-potential-of-llm-agents.pdf",
    sources: [
      "https://arxiv.org/pdf/2309.07864",
      "https://xuanjing-huang.github.io/files/agent.pdf"
    ]
  },
  efficientAgents: {
    title: "Toward Efficient Agents",
    file: "toward-efficient-agents.pdf",
    sources: ["https://arxiv.org/pdf/2601.14192"]
  },
  memoryLlmEra: {
    title: "Memory in the LLM Era",
    file: "memory-in-the-llm-era.pdf",
    sources: ["https://arxiv.org/pdf/2604.01707"]
  },
  amem: {
    title: "A-MEM: Agentic Memory for LLM Agents",
    file: "a-mem-agentic-memory.pdf",
    sources: ["https://arxiv.org/pdf/2502.12110"]
  },
  hiagent: {
    title: "HiAgent",
    file: "hiagent.pdf",
    sources: ["https://arxiv.org/pdf/2408.09559"]
  },
  agentPoison: {
    title: "AgentPoison",
    file: "agentpoison.pdf",
    sources: [
      "https://proceedings.neurips.cc/paper_files/paper/2024/file/eb113910e9c3f6242541c1652e30dfd6-Paper-Conference.pdf"
    ]
  },
  autogen: {
    title: "AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation",
    file: "autogen.pdf",
    sources: ["https://arxiv.org/pdf/2308.08155"]
  },
  metagpt: {
    title: "MetaGPT: Meta Programming for Multi-Agent Collaborative Framework",
    file: "metagpt.pdf",
    sources: ["https://arxiv.org/pdf/2308.00352"]
  },
  aflow: {
    title: "AFlow: Automating Agentic Workflow Generation",
    file: "aflow.pdf",
    sources: ["https://arxiv.org/pdf/2410.10762"]
  },
  planningSurvey: {
    title: "Understanding the Planning of LLM Agents",
    file: "understanding-the-planning-of-llm-agents.pdf",
    sources: ["https://arxiv.org/pdf/2402.02716"]
  },
  multiAgentScaling: {
    title: "Scaling Large Language Model based Multi-Agent Collaboration",
    file: "scaling-llm-multi-agent-collaboration.pdf",
    sources: ["https://arxiv.org/pdf/2406.07155"]
  },
  spaBench: {
    title: "SPA-Bench",
    file: "spa-bench.pdf",
    sources: ["https://arxiv.org/pdf/2410.15164"]
  },
  cybench: {
    title: "CyBench",
    file: "cybench.pdf",
    sources: ["https://arxiv.org/pdf/2408.08926"]
  },
  dpo: {
    title: "Direct Preference Optimization",
    file: "direct-preference-optimization.pdf",
    sources: ["https://arxiv.org/pdf/2305.18290"]
  },
  deepseekR1: {
    title: "DeepSeek-R1",
    file: "deepseek-r1.pdf",
    sources: ["https://arxiv.org/pdf/2501.12948"]
  },
  searchR1: {
    title: "Search-R1",
    file: "search-r1.pdf",
    sources: ["https://arxiv.org/pdf/2503.09516"]
  },
  agentGymRl: {
    title: "AgentGym-RL",
    file: "agentgym-rl.pdf",
    sources: [
      "https://arxiv.org/pdf/2509.08755",
      "https://openreview.net/pdf?id=ZgCCDwcGwn",
      "https://openreview.net/pdf/70ab498911db04dc212ccfbd924b2feb192554b0.pdf"
    ]
  },
  skillRl: {
    title: "SkillRL",
    file: "skillrl.pdf",
    sources: ["https://arxiv.org/pdf/2602.08234"]
  }
});

const paperSequence = [
  "transformer",
  "gpt3",
  "cot",
  "rag",
  "toolformer",
  "instructgpt",
  "autonomousAgentsSurvey",
  "riseOfAgents",
  "efficientAgents",
  "memoryLlmEra",
  "amem",
  "hiagent",
  "agentPoison",
  "toolformer",
  "efficientAgents",
  "autogen",
  "metagpt",
  "aflow",
  "planningSurvey",
  "cot",
  "multiAgentScaling",
  "spaBench",
  "cybench",
  "agentPoison",
  "dpo",
  "deepseekR1",
  "searchR1",
  "agentGymRl",
  "skillRl",
  "autogen",
  "efficientAgents"
];

const supplementalByIndex = Object.freeze({
  6: ["constitutionalAi"],
  30: ["metagpt", "aflow"]
});

export const paperLibrary = Object.freeze(
  paperSequence.map((key, offset) => Object.freeze({ index: offset + 1, key, ...paperAssets[key] }))
);

export const uniquePaperAssets = Object.freeze(
  Array.from(new Map(paperLibrary.map((paper) => [paper.file, paper])).values())
);

export const downloadablePaperAssets = Object.freeze(Object.values(paperAssets));

export function paperPdfAsset(globalIndex) {
  const asset = paperLibrary[globalIndex - 1];
  if (!asset) {
    throw new RangeError(`Unknown paper index: ${globalIndex}`);
  }
  return asset;
}

export function paperPdfPath(globalIndex) {
  return `./assets/pdfs/${paperPdfAsset(globalIndex).file}`;
}

export function paperPdfLinks(globalIndex) {
  const primary = paperPdfAsset(globalIndex);
  const supplemental = (supplementalByIndex[globalIndex] || []).map((key) => paperAssets[key]);
  return [primary, ...supplemental].map((paper) => ({
    title: paper.title,
    path: `./assets/pdfs/${paper.file}`
  }));
}
