import { readFile, stat } from "node:fs/promises";
import { architecture, metrics, modules as modulesZh } from "../src/data.js";
import { architecture as architectureEn, metrics as metricsEn, modules as modulesEn } from "../src/data.en.js";
import { DEFAULT_LOCALE, resolveLocale, ui } from "../src/i18n.js";
import {
  downloadablePaperAssets,
  paperLibrary,
  paperPdfLinks,
  paperPdfPath,
  uniquePaperAssets
} from "../src/paper-library.js";
import { practiceProjects } from "../src/practice.js";

const modules = modulesEn;
const practiceProject = practiceProjects.en;

const failures = [];

function assert(condition, message) {
  if (!condition) {
    failures.push(message);
  }
}

assert(modules.length === 8, `expected 8 modules, got ${modules.length}`);
assert(modulesZh.length === modules.length, "English and Chinese module counts should match");
assert(architecture.length >= 6, "architecture should contain at least 6 components");
assert(architectureEn.length >= 8, "English architecture should reflect the current runtime");
assert(metrics.length >= 8, "metrics should contain at least 8 evaluation fields");
assert(metricsEn.length >= 8, "English metrics should cover outcome, cost, and safety");
assert(practiceProject.phases.length === 7, "practice project should explain 7 current implementation layers");
assert(practiceProject.theoryMaps.length >= 8, "practice project should map to all major theory modules");
assert(practiceProject.codeTour.length >= 6, "practice project should include a concrete code reading path");
assert(practiceProject.lessons.length >= 6, "practice project should include engineering lessons");
assert(
  practiceProject.stats.some((item) => item.label === "Persistent stores" && item.value === "5"),
  "practice project should identify all persistent state owners"
);
assert(
  practiceProject.codeTour.some((item) => item.paths.includes("cmd/paper-agent/main.go")),
  "practice project should link to the Go CLI entrypoint"
);
assert(
  !JSON.stringify(practiceProject).includes("src/agent.js"),
  "practice project should not reference the retired Node.js runtime"
);
assert(JSON.stringify(practiceProject).includes("MCP"), "practice project should document MCP tools");
assert(JSON.stringify(practiceProject).includes("awaiting_acceptance"), "practice project should document plan acceptance");
assert(JSON.stringify(practiceProject).includes("L3"), "practice project should document session L3 compaction");
assert(practiceProjects["zh-CN"].phases.length === practiceProject.phases.length, "practice locales should have phase parity");
assert(DEFAULT_LOCALE === "en", "site default locale must be English");
assert(resolveLocale("", "") === "en", "empty locale preference must resolve to English");
assert(resolveLocale("?lang=zh-CN", "en") === "zh-CN", "query locale should override stored locale");
assert(ui.en && ui["zh-CN"], "UI copy must include English and Simplified Chinese");

const totalPapers = modules.reduce((sum, item) => sum + item.papers.length, 0);
assert(paperLibrary.length === totalPapers, "every paper entry should map to a local PDF");
assert(uniquePaperAssets.length < paperLibrary.length, "duplicate paper entries should reuse local PDFs");
assert(downloadablePaperAssets.length > uniquePaperAssets.length, "combined entries should include supplemental PDFs");
assert(paperPdfLinks(6).length === 2, "RLHF entry should include the Constitutional AI paper");
assert(paperPdfLinks(30).length === 3, "engineering entry should include AutoGen, MetaGPT, and AFlow");
for (const paper of paperLibrary) {
  assert(paper.sources.length > 0, `${paper.title} should declare a download source`);
  assert(paper.sources.every((source) => /^https:\/\//.test(source)), `${paper.title} has an invalid source`);
  assert(paperPdfPath(paper.index).startsWith("./assets/pdfs/"), `${paper.title} has an invalid local path`);
  assert(paperPdfLinks(paper.index).length >= 1, `${paper.title} should expose at least one local PDF`);
}
const ids = new Set();
const explanations = new Set();
for (const [moduleIndex, module] of modules.entries()) {
  const chineseModule = modulesZh[moduleIndex];
  assert(module.id && !ids.has(module.id), `module id must be unique: ${module.id}`);
  assert(chineseModule.id === module.id, `locale module order differs at ${module.id}`);
  ids.add(module.id);
  assert(module.title.length > 0, `module ${module.id} missing title`);
  assert(module.summary.length >= 80, `module ${module.id} summary is too short`);
  assert(module.papers.length > 0, `module ${module.id} has no papers`);
  assert(chineseModule.papers.length === module.papers.length, `${module.id} paper count differs by locale`);
  for (const [paperIndex, paper] of module.papers.entries()) {
    const chinesePaper = chineseModule.papers[paperIndex];
    assert(paper.title.length > 0, `paper in ${module.id} missing title`);
    assert(/^https?:\/\//.test(paper.url), `${paper.title} missing valid URL`);
    assert(paper.url === chinesePaper.url, `${paper.title} source URL differs by locale`);
    assert(paper.overview.length >= 120, `${paper.title} English overview is too short`);
    assert(paper.overview.length <= 500, `${paper.title} English overview is too long`);
    const englishWords = paper.explanation.match(/[A-Za-z][A-Za-z'-]*/g) || [];
    const explanationParts = paper.explanation.split(/\n{2,}/).filter(Boolean);
    const chineseCharacters = chinesePaper.explanation.match(/[\u3400-\u9fff]/g) || [];
    const chineseParts = chinesePaper.explanation.split(/\n{2,}/).filter(Boolean);
    assert(englishWords.length >= 150, `${paper.title} English explanation has fewer than 150 words`);
    assert(explanationParts.length === 4, `${paper.title} explanation should have four readable parts`);
    assert(chineseCharacters.length >= 200, `${chinesePaper.title} Chinese explanation has fewer than 200 Chinese characters`);
    assert(chineseParts.length === 4, `${chinesePaper.title} Chinese explanation should have four readable parts`);
    assert(!explanations.has(paper.explanation), `${paper.title} explanation duplicates another entry`);
    explanations.add(paper.explanation);
    assert(paper.tags.length >= 2, `${paper.title} should have tags for visual generation`);
  }
}

const html = await readFile(new URL("../index.html", import.meta.url), "utf8");
const css = await readFile(new URL("../styles.css", import.meta.url), "utf8");
const app = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
const server = await readFile(new URL("../server.mjs", import.meta.url), "utf8");

assert(html.includes('<div id="app"></div>'), "index must include app mount");
assert(html.includes("./src/app.js"), "index must load app.js");
assert(html.includes('name="viewport"'), "index must declare a responsive viewport");
assert(html.includes('<html lang="en">'), "index must default to English");
assert(css.includes(".paper-card"), "styles must include paper cards");
assert(css.includes(".paper-visual"), "styles must include paper visual cards");
assert(css.includes(".paper-image-frame"), "styles must include ImageGen paper image frames");
assert(css.includes(".notes-panel"), "styles must include notes panel");
assert(app.includes("item.papers.length"), "app must render paper counts from data");
assert(app.includes("paperVisual"), "app must render a visual for each paper");
assert(app.includes("paperImagePath"), "app must map each paper to an ImageGen asset");
assert(app.includes("paperPdfLinks"), "app must map each paper to local PDF assets");
assert(app.includes("<img"), "app must render paper visuals as image elements");
assert(app.includes("renderPaperReader"), "app must render in-app paper reader pages");
assert(app.includes("renderLocalPdf"), "app must render a local PDF reader page");
assert(app.includes("getPdfJs"), "local PDF reader should load the vendored PDF.js runtime");
assert(app.includes("pdfCanvas"), "local PDF reader should render pages to a canvas");
assert(app.includes("#/paper/"), "paper cards must link to in-app paper reader routes");
assert(app.includes("#/pdf/"), "paper source actions must link to the local PDF reader route");
assert(app.includes('t("readReview")'), "paper primary action should use localized structured analysis copy");
assert(app.includes('t("paperOverview")'), "paper reader should include a localized overview section");
assert(app.includes('t("detailedExplanation")'), "paper reader should include a localized detailed explanation section");
assert(app.includes("renderPaperExplanation"), "paper reader should render structured explanation parts");
assert(app.includes("${paper.overview}"), "paper views should render the overview field");
assert(!app.includes('target="_blank" rel="noreferrer">打开论文'), "paper primary action must not jump straight to an external site");
assert(app.includes('t("openLocalPdf")'), "paper reader should open the local PDF asset with localized copy");
assert(!app.includes('href="${paper.url}"'), "paper reader must not use the external source as its main link");
assert(app.includes("renderPractice"), "app must render the Paper Agent practice tab page");
assert(!app.includes("Verdent"), "public practice content must not depend on a private Verdent repository");
assert(app.includes("#/practice/timeline"), "app must link to the practice timeline tab");
assert(app.includes("saveSelectionNote"), "app must support saving selected text as notes");
assert(app.includes("toggle-notes"), "app must expose the compact notes drawer control");
assert(app.includes('addEventListener("touchend"'), "text selection notes must support touch devices");
assert(app.includes("localStorage"), "notes must persist in local browser storage");
assert(app.includes("languageControl"), "app must expose a language control on all major views");
assert(app.includes("setLocale"), "app must support runtime locale changes");
assert(app.includes("modulesEn") && app.includes("modulesZh"), "app must load both paper datasets");
assert(server.includes('".pdf": "application/pdf"'), "server must serve local papers as application/pdf");
assert(server.includes('".mjs": "text/javascript; charset=utf-8"'), "server must serve PDF.js modules as JavaScript");
assert(css.includes(".practice-tabs"), "styles must include practice tab navigation");
assert(css.includes(".phase-card"), "styles must include practice timeline cards");
assert(css.includes("@media (max-width: 420px)"), "styles must include a small-phone breakpoint");
assert(css.includes(".notes-mobile-toggle"), "styles must include the responsive notes drawer");
assert(!app.includes("No paper data"), "app must not contain empty paper fallback text");

for (let index = 1; index <= totalPapers; index++) {
  const fileName = `paper-${String(index).padStart(2, "0")}.png`;
  try {
    const info = await stat(new URL(`../assets/papers/${fileName}`, import.meta.url));
    assert(info.size > 10000, `${fileName} should be a non-empty ImageGen asset`);
  } catch {
    assert(false, `${fileName} is missing from assets/papers`);
  }
}

const verifyLocalPdfs = process.env.REQUIRE_LOCAL_PDFS === "1";
if (verifyLocalPdfs) {
  const { open } = await import("node:fs/promises");
  for (const paper of downloadablePaperAssets) {
    try {
      const pdfUrl = new URL(`../assets/pdfs/${paper.file}`, import.meta.url);
      const info = await stat(pdfUrl);
      const handle = await open(pdfUrl, "r");
      const header = Buffer.alloc(5);
      try {
        await handle.read(header, 0, header.length, 0);
      } finally {
        await handle.close();
      }
      assert(info.size > 10_000, `${paper.file} should be a non-empty local PDF`);
      assert(header.toString("ascii") === "%PDF-", `${paper.file} should have a valid PDF header`);
    } catch {
      assert(false, `${paper.file} is missing; run npm run papers:download`);
    }
  }
}

for (const [fileName, minimumSize] of [
  ["pdf.mjs", 100_000],
  ["pdf.worker.mjs", 500_000]
]) {
  try {
    const info = await stat(new URL(`../assets/vendor/pdfjs/${fileName}`, import.meta.url));
    assert(info.size > minimumSize, `${fileName} should be a complete vendored PDF.js asset`);
  } catch {
    assert(false, `${fileName} is missing; run npm run pdfjs:sync`);
  }
}

if (failures.length) {
  console.error("Regression check failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exit(1);
}

console.log(
  `Regression check passed: ${modules.length} modules, ${totalPapers} bilingual papers, ${totalPapers} images, ${downloadablePaperAssets.length} ${verifyLocalPdfs ? "verified local PDFs" : "local PDF definitions"}, ${practiceProject.phases.length} implementation layers.`
);
