import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const pairs = [
  ["README.md", "README.zh-CN.md"],
  ["CODE_OF_CONDUCT.md", "CODE_OF_CONDUCT.zh-CN.md"],
  ["CONTRIBUTING.md", "CONTRIBUTING.zh-CN.md"],
  ["SECURITY.md", "SECURITY.zh-CN.md"],
  ["agent_research_map_from_feishu_urls.md", "agent_research_map_from_feishu_urls.zh-CN.md"],
  ["feishu_agent_urls.md", "feishu_agent_urls.zh-CN.md"],
  ["ai-agent-roadmap-site/assets/pdfs/README.md", "ai-agent-roadmap-site/assets/pdfs/README.zh-CN.md"],
  ["assets/architecture/README.md", "assets/architecture/README.zh-CN.md"],
  ["docs/architecture.md", "docs/architecture.zh-CN.md"],
  ["docs/engineering-practice.md", "docs/engineering-practice.zh-CN.md"],
  ["docs/feishu-adapter.md", "docs/feishu-adapter.zh-CN.md"],
  ["docs/feishu-agent-papers-and-projects.md", "docs/feishu-agent-papers-and-projects.zh-CN.md"],
  ["docs/i18n.md", "docs/i18n.zh-CN.md"],
  ["docs/paper-reading-template.md", "docs/paper-reading-template.zh-CN.md"],
  ["docs/roadmap.md", "docs/roadmap.zh-CN.md"],
  ["examples/your-agent/README.md", "examples/your-agent/README.zh-CN.md"],
  ["skills/paper-research/SKILL.md", "skills/paper-research/SKILL.zh-CN.md"]
];

const inlineBilingual = new Set([".github/pull_request_template.md"]);
const bilingualIssueForms = [
  ".github/ISSUE_TEMPLATE/paper.yml",
  ".github/ISSUE_TEMPLATE/engineering.yml"
];
const excluded = new Set(["ai-agent-roadmap-site/assets/vendor/pdfjs/NOTICE.md"]);
const failures = [];

function fail(message) {
  failures.push(message);
}

async function read(relative) {
  try {
    const content = await readFile(path.join(root, relative), "utf8");
    if (!content.trim()) fail(`${relative} is empty`);
    return content;
  } catch (error) {
    fail(`${relative} is missing: ${error.code || error.message}`);
    return "";
  }
}

for (const [englishPath, chinesePath] of pairs) {
  const [english, chinese] = await Promise.all([read(englishPath), read(chinesePath)]);
  const englishName = path.basename(englishPath);
  const chineseName = path.basename(chinesePath);
  if (english && (!english.includes("[English]") || !english.includes(chineseName))) {
    fail(`${englishPath} must link to English and ${chineseName}`);
  }
  if (chinese && (!chinese.includes(englishName) || !chinese.includes("简体中文"))) {
    fail(`${chinesePath} must link back to ${englishName} and identify Simplified Chinese`);
  }
}

async function walk(relative = "") {
  const entries = await readdir(path.join(root, relative), { withFileTypes: true });
  const result = [];
  for (const entry of entries) {
    const child = path.join(relative, entry.name).split(path.sep).join("/");
    if (entry.isDirectory()) {
      if ([".git", ".gocache", ".agent-data", "dist", "node_modules", "ai-agent-roadmap-android"].includes(entry.name)) continue;
      result.push(...(await walk(child)));
    } else if (entry.isFile() && entry.name.endsWith(".md")) {
      result.push(child);
    }
  }
  return result;
}

const pairedFiles = new Set(pairs.flat());
for (const markdown of await walk()) {
  if (!pairedFiles.has(markdown) && !inlineBilingual.has(markdown) && !excluded.has(markdown)) {
    fail(`unclassified Markdown document: ${markdown}`);
  }
}

for (const file of inlineBilingual) {
  const content = await read(file);
  if (!content.includes("English") || !content.includes("中文")) {
    fail(`${file} must contain English-first and Chinese guidance`);
  }
}

for (const file of bilingualIssueForms) {
  const content = await read(file);
  if (!/^name: [^\n]+ \/ [^\n]+/m.test(content) || !content.includes("description:")) {
    fail(`${file} must use English-first bilingual labels`);
  }
}

const corpusIndex = await read("docs/feishu-agent-papers-and-projects.md");
if (!corpusIndex.includes("source-language mirror") || !corpusIndex.includes("feishu-agent-papers-and-projects.zh-CN.md")) {
  fail("the English corpus index must explain and link the source-language mirror");
}

const index = await read("ai-agent-roadmap-site/index.html");
if (!index.includes('<html lang="en">')) fail("the interactive site must default to English");

const packageJson = JSON.parse(await read("package.json"));
if (!packageJson.description?.includes("English-default bilingual")) {
  fail("package description must identify the English-default bilingual project");
}

if (failures.length) {
  console.error("Documentation locale check failed:");
  failures.forEach((message) => console.error(`- ${message}`));
  process.exit(1);
}

console.log(`Documentation locale check passed: ${pairs.length} bilingual pairs, ${inlineBilingual.size} Markdown template, and ${bilingualIssueForms.length} issue forms.`);
