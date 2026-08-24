import { architecture as architectureZh, metrics as metricsZh, modules as modulesZh } from "./data.js";
import { architecture as architectureEn, metrics as metricsEn, modules as modulesEn } from "./data.en.js";
import {
  DEFAULT_LOCALE,
  LOCALE_STORAGE_KEY,
  resolveLocale,
  ui
} from "./i18n.js";
import { paperPdfLinks } from "./paper-library.js";
import { practiceProjects } from "./practice.js";

const app = document.querySelector("#app");
const NOTES_KEY = "ai-agent-roadmap-notes-v1";
let locale = resolveLocale(window.location.search, localStorage.getItem(LOCALE_STORAGE_KEY));
let modules;
let architecture;
let metrics;
let practiceProject;
let selectionState = null;
let pdfJsPromise = null;
let pdfReaderState = {
  document: null,
  loadingTask: null,
  renderTask: null,
  pageNumber: 1,
  zoom: 1,
  requestId: 0
};

function applyLocaleData() {
  const english = locale === DEFAULT_LOCALE;
  modules = english ? modulesEn : modulesZh;
  architecture = english ? architectureEn : architectureZh;
  metrics = english ? metricsEn : metricsZh;
  practiceProject = practiceProjects[locale];
  document.documentElement.lang = locale;
  document.title = "AI Agent Roadmap";
  document.querySelector('meta[name="description"]')?.setAttribute("content", ui[locale].description);
}

function t(key) {
  return ui[locale][key];
}

function setLocale(nextLocale) {
  if (!ui[nextLocale] || nextLocale === locale) return;
  locale = nextLocale;
  localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  const url = new URL(window.location.href);
  url.searchParams.set("lang", locale);
  window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  applyLocaleData();
  document.querySelector("[data-action='save-selection-note']")?.replaceChildren(t("saveSelection"));
  render();
}

function languageControl() {
  return `
    <div class="language-control" role="group" aria-label="Language">
      <button type="button" data-action="set-locale" data-locale="en" class="${locale === "en" ? "active" : ""}" aria-pressed="${locale === "en"}">English</button>
      <button type="button" data-action="set-locale" data-locale="zh-CN" class="${locale === "zh-CN" ? "active" : ""}" aria-pressed="${locale === "zh-CN"}">简体中文</button>
    </div>
  `;
}

applyLocaleData();

function $(selector, root = document) {
  return root.querySelector(selector);
}

function moduleById(id) {
  return modules.find((item) => item.id === id) || modules[0];
}

function paperRoute(moduleId, paperIndex) {
  return `#/paper/${moduleId}/${paperIndex}`;
}

function paperPdfRoute(moduleId, paperIndex, pdfIndex = 0) {
  return `#/pdf/${moduleId}/${paperIndex}/${pdfIndex}`;
}

function paperByRoute(moduleId, paperIndex) {
  const module = moduleById(moduleId);
  const index = Number.parseInt(paperIndex, 10);
  const safeIndex = Number.isInteger(index) && module.papers[index] ? index : 0;
  return {
    module,
    paper: module.papers[safeIndex],
    index: safeIndex
  };
}

function paperCount() {
  return modules.reduce((sum, item) => sum + item.papers.length, 0);
}

function paperGlobalIndex(module, paperIndex) {
  const moduleIndex = modules.findIndex((entry) => entry.id === module.id);
  const previousModules = moduleIndex > 0 ? modules.slice(0, moduleIndex) : [];
  const offset = previousModules.reduce((sum, item) => sum + item.papers.length, 0);
  return offset + paperIndex + 1;
}

function paperImagePath(module, paperIndex) {
  return `./assets/papers/paper-${String(paperGlobalIndex(module, paperIndex)).padStart(2, "0")}.png`;
}

function renderPaperExplanation(explanation) {
  return String(explanation)
    .split(/\n{2,}/)
    .map((part) => {
      const match = part.match(/^([^：:]{2,32})[：:]([\s\S]+)$/);
      if (!match) {
        return `<p>${part}</p>`;
      }
      return `
        <div class="paper-explanation-part">
          <h3>${match[1]}</h3>
          <p>${match[2].trim()}</p>
        </div>
      `;
    })
    .join("");
}

function route() {
  const hash = window.location.hash.replace(/^#/, "");
  if (hash.startsWith("/pdf/")) {
    const [, , moduleId, paperIndex, pdfIndex] = hash.split("/");
    return { name: "pdf", moduleId, paperIndex, pdfIndex };
  }
  if (hash.startsWith("/paper/")) {
    const [, , moduleId, paperIndex] = hash.split("/");
    return { name: "paper", moduleId, paperIndex };
  }
  if (hash.startsWith("/module/")) {
    return { name: "module", id: hash.split("/").pop() };
  }
  if (hash === "/architecture") {
    return { name: "architecture" };
  }
  if (hash.startsWith("/practice")) {
    const tab = hash.split("/")[2] || practiceProject.tabs[0].id;
    return { name: "practice", tab };
  }
  return { name: "home" };
}

function render() {
  const current = route();
  if (current.name === "pdf") {
    renderLocalPdf(current.moduleId, current.paperIndex, current.pdfIndex);
  } else if (current.name === "paper") {
    renderPaperReader(current.moduleId, current.paperIndex);
  } else if (current.name === "module") {
    renderModule(moduleById(current.id));
  } else if (current.name === "architecture") {
    renderArchitecture();
  } else if (current.name === "practice") {
    renderPractice(current.tab);
  } else {
    renderHome();
  }
  ensureSelectionToolbar();
  renderNotes();
  window.scrollTo({ top: 0, behavior: "instant" });
}

function renderHome() {
  app.innerHTML = `
    <header class="hero">
      <nav class="topbar" aria-label="${t("mainNav")}">
        <a class="brand" href="#/">
          <span class="brand-mark" aria-hidden="true">A</span>
          <span>AI Agent Roadmap</span>
        </a>
        <div class="top-actions">
          <a href="#/practice/timeline">${t("referenceImplementation")}</a>
          <a href="#/architecture">${t("systemArchitecture")}</a>
          <a href="#modules">${t("topicIndex")}</a>
          ${languageControl()}
        </div>
      </nav>
      <section class="hero-grid">
        <div class="hero-copy">
          <p class="eyebrow">Research Map · Reference Runtime</p>
          <h1>AI Agent Roadmap</h1>
          <p class="hero-lead">
            ${t("heroLead")}
          </p>
          <div class="hero-buttons">
            <a class="primary-button" href="#modules">${t("browseMap")}</a>
            <a class="secondary-button" href="#/practice/timeline">${t("viewImplementation")}</a>
            <a class="secondary-button" href="#/architecture">${t("viewArchitecture")}</a>
          </div>
        </div>
        <div class="map-visual" aria-label="${t("capabilityMap")}">
          ${modules
            .map(
              (item) => `
              <a class="map-node ${item.theme}" href="#/module/${item.id}">
                <span>${item.order}</span>
                <strong>${item.title}</strong>
              </a>`
            )
            .join("")}
        </div>
      </section>
    </header>

    <main>
      <section class="stats-band" aria-label="${t("siteStats")}">
        <article><strong>${modules.length}</strong><span>${t("researchTopics")}</span></article>
        <article><strong>${paperCount()}</strong><span>${t("representativePapers")}</span></article>
        <article><strong>${architecture.length}</strong><span>${t("engineeringComponents")}</span></article>
        <article><strong>${metrics.length}</strong><span>${t("evaluationMetrics")}</span></article>
      </section>

      <section class="section" id="modules">
        <div class="section-heading">
          <p class="eyebrow">Research Map</p>
          <h2>${t("coreTopicsTitle")}</h2>
          <p>${t("coreTopicsBody")}</p>
        </div>
        <div class="module-grid">
          ${modules.map(moduleCard).join("")}
        </div>
      </section>

      <section class="section split-section">
        <div>
          <p class="eyebrow">Build Direction</p>
          <h2>${t("paperAgentTitle")}</h2>
          <p>${t("paperAgentBody")}</p>
        </div>
        <div class="checklist">
          ${t("checklist").map((item) => `<span>${item}</span>`).join("")}
        </div>
      </section>

      <section class="section practice-home-band">
        <div>
          <p class="eyebrow">Reference Runtime</p>
          <h2>${t("completeLoopTitle")}</h2>
          <p>${t("completeLoopBody")}</p>
        </div>
        <a class="primary-button" href="#/practice/timeline">${t("viewBuild")}</a>
      </section>
    </main>
  `;
}

function moduleCard(item) {
  return `
    <article class="module-card ${item.theme}">
      <div class="module-order">${item.order}</div>
      <h3>${item.title}</h3>
      <p>${item.subtitle}</p>
      <div class="card-meta">
        <span>${item.papers.length} ${t("papers")}</span>
        <span>${item.theme}</span>
      </div>
      <a href="#/module/${item.id}" aria-label="${t("viewTopic")} ${item.title}">${t("viewTopic")}</a>
    </article>
  `;
}

function renderModule(item) {
  app.innerHTML = `
    <header class="detail-header">
      <a class="back-link" href="#/">${t("back")}</a>
      <div>
        <h1>${item.title}</h1>
        <p>${item.papers.length} ${t("papers")}</p>
      </div>
      ${languageControl()}
    </header>
    <main class="detail-main module-detail-main">
      <section class="module-study-layout">
        <div class="module-content">
          <section class="intro-card note-source" data-note-source="${item.title} ${t("topicOverview")}">
            <h2>${t("topicOverview")}</h2>
            <p>${item.summary}</p>
            <p>${item.build}</p>
          </section>

          <section class="section">
            <div class="section-heading compact">
              <h2>${t("corePapers")}</h2>
              <p>${t("corePapersBody")}</p>
            </div>
            <div class="paper-list">
              ${item.papers.map((paper, index) => paperCard(paper, item, index)).join("")}
            </div>
          </section>
        </div>

        ${notesPanelMarkup()}
      </section>

      <nav class="detail-nav" aria-label="${t("moduleNavigation")}">
        ${neighborLink(item, -1)}
        <a class="primary-button" href="#/architecture">${t("architectureDesign")}</a>
        ${neighborLink(item, 1)}
      </nav>
    </main>
  `;
}

function paperCard(paper, module, index) {
  return `
    <article class="paper-card note-source" data-note-source="${paper.title}">
      <div class="paper-layout">
        ${paperVisual(paper, module, index)}
        <div class="paper-copy">
          <div class="paper-topline">
            <h3>${paper.title}</h3>
            <a href="${paperRoute(module.id, index)}">${t("readReview")}</a>
          </div>
          <div class="tag-row">${paper.tags.map((tag) => `<span>${tag}</span>`).join("")}</div>
          <p class="paper-card-kicker">${t("paperOverview")}</p>
          <p>${paper.overview}</p>
          <div class="paper-deep-dive">
            <section>
              <h4>${t("whatToInspect")}</h4>
              <p>${readingGuide(paper, module)}</p>
            </section>
            <section>
              <h4>${t("implementationMapping")}</h4>
              <p>${practiceGuide(paper, module)}</p>
            </section>
          </div>
        </div>
      </div>
    </article>
  `;
}

function renderPaperReader(moduleId, paperIndex) {
  const { module, paper, index } = paperByRoute(moduleId, paperIndex);
  const globalIndex = paperGlobalIndex(module, index);
  const localPdfs = paperPdfLinks(globalIndex);
  app.innerHTML = `
    <header class="detail-header">
      <a class="back-link" href="#/module/${module.id}">${t("backToTopic")} ${module.title}</a>
      <div>
        <h1>${paper.title}</h1>
        <p>${module.title} · ${t("paper")} ${String(globalIndex).padStart(2, "0")}</p>
      </div>
      ${languageControl()}
    </header>
    <main class="detail-main module-detail-main">
      <section class="module-study-layout paper-reader-layout">
        <article class="paper-reader note-source" data-note-source="${paper.title} ${t("paperAnalysis")}">
          <div class="paper-reader-hero">
            ${paperVisual(paper, module, index)}
            <div class="paper-reader-summary">
              <p class="eyebrow">Structured Paper Review</p>
              <h2>${t("reviewHeroTitle")}</h2>
              <p>${t("reviewHeroBody")}</p>
              <div class="tag-row">${paper.tags.map((tag) => `<span>${tag}</span>`).join("")}</div>
            </div>
          </div>

          <section class="paper-reader-section">
            <h2>${t("paperOverview")}</h2>
            <p>${paper.overview}</p>
          </section>

          <section class="paper-reader-section paper-reader-detail">
            <h2>${t("detailedExplanation")}</h2>
            <div class="paper-explanation">${renderPaperExplanation(paper.explanation)}</div>
          </section>

          <section class="paper-reader-grid">
            <div class="paper-reader-section">
              <h2>${t("systemRelevance")}</h2>
              <p>${readingGuide(paper, module)}</p>
              <p>${module.summary}</p>
            </div>
            <div class="paper-reader-section">
              <h2>${t("implementationMapping")}</h2>
              <p>${practiceGuide(paper, module)}</p>
              <p>${module.build}</p>
            </div>
          </section>

          <section class="paper-reader-section source-section">
            <h2>${t("localPaper")}</h2>
            <p>${t("localPaperBody")}</p>
            <div class="source-actions">
              ${localPdfs
                .map(
                  (item, pdfIndex) =>
                    `<a class="secondary-button" href="${paperPdfRoute(module.id, index, pdfIndex)}">${
                      localPdfs.length === 1 ? t("openLocalPdf") : `${t("openNamedPaper")} ${item.title}`
                    }</a>`
                )
                .join("")}
            </div>
          </section>
        </article>

        ${notesPanelMarkup()}
      </section>

      <nav class="detail-nav" aria-label="${t("paperNavigation")}">
        ${paperNeighborLink(module, index, -1)}
        <a class="primary-button" href="#/module/${module.id}">${t("backToModule")}</a>
        ${paperNeighborLink(module, index, 1)}
      </nav>
    </main>
  `;
}

function renderLocalPdf(moduleId, paperIndex, pdfIndex) {
  const { module, paper, index } = paperByRoute(moduleId, paperIndex);
  const localPdfs = paperPdfLinks(paperGlobalIndex(module, index));
  const requestedIndex = Number.parseInt(pdfIndex, 10);
  const safePdfIndex = Number.isInteger(requestedIndex) && localPdfs[requestedIndex] ? requestedIndex : 0;
  const localPdf = localPdfs[safePdfIndex];

  app.innerHTML = `
    <header class="detail-header local-pdf-header">
      <a class="back-link" href="${paperRoute(module.id, index)}">${t("backToAnalysis")}</a>
      <div>
        <p class="eyebrow">Local Paper Cache</p>
        <h1>${localPdf.title}</h1>
        <p>${paper.title} · ${t("localPdf")} ${safePdfIndex + 1}/${localPdfs.length}</p>
      </div>
      <div class="detail-header-actions">
        ${languageControl()}
        <a class="secondary-button" href="${localPdf.path}" download>${t("downloadPdf")}</a>
      </div>
    </header>
    <main class="local-pdf-main">
      <section class="pdf-toolbar" aria-label="${t("pdfControls")}">
        <div class="pdf-control-group">
          <button class="pdf-icon-button" type="button" data-action="pdf-prev" title="${t("previousPage")}" aria-label="${t("previousPage")}" disabled>←</button>
          <output id="pdfPageStatus">${t("pageUnknown")}</output>
          <button class="pdf-icon-button" type="button" data-action="pdf-next" title="${t("nextPage")}" aria-label="${t("nextPage")}" disabled>→</button>
        </div>
        <div class="pdf-control-group">
          <button class="pdf-icon-button" type="button" data-action="pdf-zoom-out" title="${t("zoomOut")}" aria-label="${t("zoomOut")}" disabled>−</button>
          <output id="pdfZoomStatus">100%</output>
          <button class="pdf-icon-button" type="button" data-action="pdf-zoom-in" title="${t("zoomIn")}" aria-label="${t("zoomIn")}" disabled>+</button>
        </div>
        <p id="pdfLoadStatus" role="status">${t("loadingPdf")}</p>
      </section>
      <div class="pdf-canvas-shell">
        <canvas id="pdfCanvas" aria-label="${localPdf.title} ${t("currentPage")}"></canvas>
      </div>
      <p id="pdfError" class="pdf-error" hidden></p>
    </main>
  `;

  void loadLocalPdf(localPdf.path);
}

async function getPdfJs() {
  if (!pdfJsPromise) {
    const pdfJsUrl = new URL("./assets/vendor/pdfjs/pdf.mjs", document.baseURI).href;
    pdfJsPromise = import(pdfJsUrl).then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerSrc = new URL(
        "./assets/vendor/pdfjs/pdf.worker.mjs",
        document.baseURI
      ).href;
      return pdfjs;
    });
  }
  return pdfJsPromise;
}

async function resetPdfReader() {
  pdfReaderState.requestId += 1;
  pdfReaderState.renderTask?.cancel();
  await pdfReaderState.loadingTask?.destroy?.();
  await pdfReaderState.document?.destroy?.();
  pdfReaderState = {
    document: null,
    loadingTask: null,
    renderTask: null,
    pageNumber: 1,
    zoom: 1,
    requestId: pdfReaderState.requestId
  };
}

async function loadLocalPdf(path) {
  await resetPdfReader();
  const requestId = pdfReaderState.requestId;
  const loadStatus = $("#pdfLoadStatus");
  const error = $("#pdfError");
  try {
    const pdfjs = await getPdfJs();
    if (requestId !== pdfReaderState.requestId) return;
    pdfReaderState.loadingTask = pdfjs.getDocument({
      url: path,
      cMapUrl: "./assets/vendor/pdfjs/cmaps/",
      cMapPacked: true,
      standardFontDataUrl: "./assets/vendor/pdfjs/standard_fonts/",
      wasmUrl: "./assets/vendor/pdfjs/wasm/"
    });
    pdfReaderState.document = await pdfReaderState.loadingTask.promise;
    if (requestId !== pdfReaderState.requestId) return;
    loadStatus.textContent = `${t("localPdfPages")} · ${pdfReaderState.document.numPages} ${t("pages")}`;
    setPdfControlsEnabled(true);
    await renderPdfPage();
  } catch (loadError) {
    if (requestId !== pdfReaderState.requestId) return;
    loadStatus.textContent = t("pdfFailed");
    error.hidden = false;
    error.textContent = `${t("pdfRenderFailed")}: ${loadError.message}`;
  }
}

async function renderPdfPage() {
  const pdf = pdfReaderState.document;
  const canvas = $("#pdfCanvas");
  const shell = canvas?.closest(".pdf-canvas-shell");
  if (!pdf || !canvas || !shell) return;

  pdfReaderState.renderTask?.cancel();
  const page = await pdf.getPage(pdfReaderState.pageNumber);
  const naturalViewport = page.getViewport({ scale: 1 });
  const availableWidth = Math.max(320, shell.clientWidth - 40);
  const fitScale = Math.min(availableWidth / naturalViewport.width, 1.8);
  const viewport = page.getViewport({ scale: fitScale * pdfReaderState.zoom });
  const outputScale = Math.min(window.devicePixelRatio || 1, 2);
  const context = canvas.getContext("2d", { alpha: false });

  canvas.width = Math.floor(viewport.width * outputScale);
  canvas.height = Math.floor(viewport.height * outputScale);
  canvas.style.width = `${Math.floor(viewport.width)}px`;
  canvas.style.height = `${Math.floor(viewport.height)}px`;

  const transform = outputScale === 1 ? null : [outputScale, 0, 0, outputScale, 0, 0];
  pdfReaderState.renderTask = page.render({ canvasContext: context, viewport, transform });
  try {
    await pdfReaderState.renderTask.promise;
  } catch (renderError) {
    if (renderError?.name !== "RenderingCancelledException") throw renderError;
  }
  updatePdfControls();
}

function setPdfControlsEnabled(enabled) {
  document
    .querySelectorAll("[data-action^='pdf-']")
    .forEach((button) => (button.disabled = !enabled));
}

function updatePdfControls() {
  const pdf = pdfReaderState.document;
  if (!pdf) return;
  $("#pdfPageStatus").textContent = locale === "zh-CN"
    ? `${t("pageOf")} ${pdfReaderState.pageNumber} / ${pdf.numPages} ${t("pages")}`
    : `${t("pageOf")} ${pdfReaderState.pageNumber} / ${pdf.numPages}`;
  $("#pdfZoomStatus").textContent = `${Math.round(pdfReaderState.zoom * 100)}%`;
  $("[data-action='pdf-prev']").disabled = pdfReaderState.pageNumber <= 1;
  $("[data-action='pdf-next']").disabled = pdfReaderState.pageNumber >= pdf.numPages;
}

function changePdfPage(delta) {
  const pdf = pdfReaderState.document;
  if (!pdf) return;
  pdfReaderState.pageNumber = Math.max(1, Math.min(pdf.numPages, pdfReaderState.pageNumber + delta));
  void renderPdfPage();
}

function changePdfZoom(delta) {
  if (!pdfReaderState.document) return;
  pdfReaderState.zoom = Math.max(0.6, Math.min(2.4, pdfReaderState.zoom + delta));
  void renderPdfPage();
}

function paperVisual(paper, module, index) {
  const globalIndex = paperGlobalIndex(module, index);
  const caption = visualCaption(paper, module);
  return `
    <figure class="paper-visual ${module.theme}" aria-label="${paper.title} ${t("conceptVisual")}">
      <div class="paper-image-frame">
        <img
          src="${paperImagePath(module, index)}"
          alt="${t("conceptVisualAlt")} ${paper.title}"
          width="1440"
          height="1080"
          loading="eager"
        />
      </div>
      <figcaption>
        <span>${t("paperVisual")} ${String(globalIndex).padStart(2, "0")}</span>
        <strong>${visualTitle(paper)}</strong>
        <em>${caption}</em>
      </figcaption>
    </figure>
  `;
}

function paperNeighborLink(module, paperIndex, offset) {
  const nextIndex = paperIndex + offset;
  const next = module.papers[nextIndex];
  if (!next) {
    return `<span class="nav-placeholder"></span>`;
  }
  const label = offset < 0 ? `← ${next.title}` : `${next.title} →`;
  return `<a class="secondary-button" href="${paperRoute(module.id, nextIndex)}">${label}</a>`;
}

function visualTitle(paper) {
  const titles = t("visualTitles");
  if (paper.title.includes("Attention")) return titles[0];
  if (paper.title.includes("Few-Shot")) return titles[1];
  if (paper.title.includes("Chain-of-Thought")) return titles[2];
  if (paper.title.includes("Retrieval")) return titles[3];
  if (paper.title.includes("Toolformer")) return titles[4];
  if (paper.title.includes("RLHF") || paper.title.includes("Constitutional")) return titles[5];
  if (paper.title.includes("A-MEM")) return titles[6];
  if (paper.title.includes("HiAgent")) return titles[7];
  if (paper.title.includes("Poison")) return titles[8];
  if (paper.title.includes("AutoGen")) return titles[9];
  if (paper.title.includes("MetaGPT")) return titles[10];
  if (paper.title.includes("AFlow")) return titles[11];
  if (paper.title.includes("Planning")) return titles[12];
  if (paper.title.includes("Scaling")) return titles[13];
  if (paper.title.includes("SPA")) return titles[14];
  if (paper.title.includes("CyBench")) return titles[15];
  if (paper.title.includes("Preference")) return titles[16];
  if (paper.title.includes("DeepSeek")) return titles[17];
  if (paper.title.includes("Search")) return titles[18];
  if (paper.title.includes("AgentGym")) return titles[19];
  if (paper.title.includes("SkillRL")) return titles[20];
  return t("defaultVisualTitle");
}

function visualCaption(paper, module) {
  const tagText = paper.tags.slice(0, 2).join(" + ");
  return t("visualCaption")(tagText, module.title);
}

function readingGuide(paper, module) {
  return t("readingGuide")(module.title);
}

function practiceGuide(paper, module) {
  return t("practiceGuide")(paper.tags[0]);
}

function neighborLink(item, offset) {
  const index = modules.findIndex((entry) => entry.id === item.id);
  const next = modules[index + offset];
  if (!next) {
    return `<span class="nav-placeholder"></span>`;
  }
  const label = offset < 0 ? `← ${next.title}` : `${next.title} →`;
  return `<a class="secondary-button" href="#/module/${next.id}">${label}</a>`;
}

function renderPractice(tabId) {
  const activeTab = practiceProject.tabs.some((tab) => tab.id === tabId) ? tabId : practiceProject.tabs[0].id;
  app.innerHTML = `
    <header class="detail-header">
      <a class="back-link" href="#/">${t("back")}</a>
      <div>
        <h1>${t("referenceTitle")}: ${practiceProject.name}</h1>
        <p>${practiceProject.subtitle}</p>
      </div>
      ${languageControl()}
    </header>
    <main class="detail-main practice-detail-main">
      <section class="intro-card note-source" data-note-source="${practiceProject.name} ${t("referenceTitle")}">
        <h2>${t("practiceIntro")}</h2>
        <p>${practiceProject.summary}</p>
        <p>${t("repositoryPath")}: <code>${practiceProject.repoPath}</code></p>
      </section>

      <section class="practice-stat-grid" aria-label="${practiceProject.name} ${t("projectStats")}">
        ${practiceProject.stats
          .map(
            (item) => `
            <article>
              <strong>${item.value}</strong>
              <span>${item.label}</span>
              <p>${item.note}</p>
            </article>`
          )
          .join("")}
      </section>

      <nav class="practice-tabs" aria-label="${t("practiceTabs")}">
        ${practiceProject.tabs
          .map(
            (tab) => `
            <a class="${tab.id === activeTab ? "active" : ""}" href="#/practice/${tab.id}">${tab.label}</a>`
          )
          .join("")}
      </nav>

      <section class="practice-panel note-source" data-note-source="${practiceProject.name} ${practiceTabLabel(activeTab)}">
        ${practicePanel(activeTab)}
      </section>

      ${notesPanelMarkup("notes-panel--floating")}
    </main>
  `;
}

function notesPanelMarkup(extraClass = "") {
  return `
    <aside class="notes-panel ${extraClass}" aria-label="${t("researchNotes")}">
      <button class="notes-mobile-toggle" type="button" data-action="toggle-notes" aria-expanded="false">
        <span>
          <strong>${t("researchNotes")}</strong>
          <small id="notesCountBadge">0 ${t("notesCount")}</small>
        </span>
        <span class="notes-toggle-icon" aria-hidden="true">↑</span>
      </button>
      <div class="notes-panel-content">
        <div class="notes-heading">
          <p class="eyebrow">Notes</p>
          <h2>${t("researchNotes")}</h2>
          <p>${t("notesHelp")}</p>
        </div>
        <div id="notesList" class="notes-list"></div>
      </div>
    </aside>
  `;
}

function practiceTabLabel(tabId) {
  return practiceProject.tabs.find((tab) => tab.id === tabId)?.label || t("referenceTitle");
}

function practicePanel(tabId) {
  if (tabId === "theory") {
    return `
      <div class="section-heading compact">
        <p class="eyebrow">Theory Mapping</p>
        <h2>${t("theoryTitle")}</h2>
        <p>${t("theoryBody")}</p>
      </div>
      <div class="theory-map-grid">
        ${practiceProject.theoryMaps.map(theoryMapCard).join("")}
      </div>
    `;
  }
  if (tabId === "code") {
    return `
      <div class="section-heading compact">
        <p class="eyebrow">Code Tour</p>
        <h2>${t("codeTitle")}</h2>
        <p>${t("codeBody")}</p>
      </div>
      <div class="code-tour-list">
        ${practiceProject.codeTour.map(codeTourItem).join("")}
      </div>
    `;
  }
  if (tabId === "lessons") {
    return `
      <div class="section-heading compact">
        <p class="eyebrow">Build Lessons</p>
        <h2>${t("lessonsTitle")}</h2>
        <p>${t("lessonsBody")}</p>
      </div>
      <div class="lesson-grid">
        ${practiceProject.lessons.map(lessonCard).join("")}
      </div>
    `;
  }
  return `
    <div class="section-heading compact">
      <p class="eyebrow">Build Timeline</p>
      <h2>${t("timelineTitle")}</h2>
      <p>${t("timelineBody")}</p>
    </div>
    <div class="practice-timeline">
      ${practiceProject.phases.map(phaseCard).join("")}
    </div>
  `;
}

function phaseCard(phase) {
  return `
    <article class="phase-card">
      <div class="phase-index">${phase.order}</div>
      <div class="phase-body">
        <div class="phase-topline">
          <div>
            <p>${phase.date}</p>
            <h3>${phase.title}</h3>
          </div>
          <span>${phase.commits}</span>
        </div>
        <section>
          <h4>${t("keyCode")}</h4>
          <p>${phase.gitSignal}</p>
        </section>
        <section>
          <h4>${t("whatBuilt")}</h4>
          <p>${phase.built}</p>
        </section>
        <section>
          <h4>${t("correspondingTheory")}</h4>
          <p>${phase.theory}</p>
        </section>
        <section>
          <h4>${t("engineeringMeaning")}</h4>
          <p>${phase.practice}</p>
        </section>
      </div>
    </article>
  `;
}

function theoryMapCard(item) {
  return `
    <article class="theory-map-card">
      <h3>${item.theory}</h3>
      <p class="implementation">${item.implementation}</p>
      <div class="path-row">${pathBadges(item.files)}</div>
      <p>${item.explanation}</p>
    </article>
  `;
}

function codeTourItem(item, index) {
  return `
    <article class="code-tour-item">
      <span>${String(index + 1).padStart(2, "0")}</span>
      <div>
        <h3>${item.area}</h3>
        <div class="path-row">${pathBadges(item.paths)}</div>
        <p>${item.read}</p>
      </div>
    </article>
  `;
}

function lessonCard(item) {
  return `
    <article class="lesson-card">
      <h3>${item.title}</h3>
      <p>${item.text}</p>
    </article>
  `;
}

function pathBadges(paths) {
  return paths.map((path) => `<code>${path}</code>`).join("");
}

function renderArchitecture() {
  app.innerHTML = `
    <header class="detail-header">
      <a class="back-link" href="#/">${t("back")}</a>
      <div>
        <h1>${t("architectureTitle")}</h1>
        <p>${t("architectureSubtitle")}</p>
      </div>
      ${languageControl()}
    </header>
    <main class="detail-main">
      <section class="intro-card note-source" data-note-source="${t("architectureTitle")}">
        <h2>${t("architectureIntroTitle")}</h2>
        <p>${t("architectureIntroBody")}</p>
      </section>

      <section class="architecture-flow" aria-label="${t("architectureComponents")}">
        ${architecture
          .map(
            (item, index) => `
            <article>
              <span>${String(index + 1).padStart(2, "0")}</span>
              <h2>${item.name}</h2>
              <p>${item.detail}</p>
            </article>`
          )
          .join("")}
      </section>

      <section class="section split-section">
        <div>
          <p class="eyebrow">Regression Metrics</p>
          <h2>${t("metricsTitle")}</h2>
          <p>${t("metricsBody")}</p>
        </div>
        <div class="metric-list">
          ${metrics.map((metric) => `<code>${metric}</code>`).join("")}
        </div>
      </section>
    </main>
  `;
}

window.addEventListener("hashchange", render);
window.addEventListener("DOMContentLoaded", render);
document.addEventListener("mouseup", handleTextSelection);
document.addEventListener("keyup", handleTextSelection);
document.addEventListener("touchend", () => window.setTimeout(handleTextSelection, 80), { passive: true });
document.addEventListener("click", handleDocumentClick);
document.addEventListener("input", handleNoteInput);

function ensureSelectionToolbar() {
  if ($("#selectionToolbar")) return;
  const toolbar = document.createElement("div");
  toolbar.id = "selectionToolbar";
  toolbar.className = "selection-toolbar hidden";
  toolbar.innerHTML = `<button type="button" data-action="save-selection-note">${t("saveSelection")}</button>`;
  document.body.appendChild(toolbar);
}

function handleTextSelection() {
  const selection = window.getSelection();
  const text = selection?.toString().replace(/\s+/g, " ").trim() || "";
  const toolbar = $("#selectionToolbar");
  if (!toolbar || text.length < 6) {
    hideSelectionToolbar();
    return;
  }
  const anchor = selection.anchorNode?.nodeType === Node.TEXT_NODE ? selection.anchorNode.parentElement : selection.anchorNode;
  const source = anchor?.closest?.(".note-source");
  if (!source) {
    hideSelectionToolbar();
    return;
  }
  const range = selection.getRangeAt(0);
  const rect = range.getBoundingClientRect();
  const moduleTitle = $(".detail-header h1")?.textContent || "AI Agent Roadmap";
  selectionState = {
    text,
    moduleTitle,
    sourceTitle: source.dataset.noteSource || moduleTitle,
    route: window.location.hash || "#/"
  };
  toolbar.style.left = `${Math.max(12, Math.min(rect.left, window.innerWidth - 128))}px`;
  toolbar.style.top = `${Math.max(12, Math.min(rect.top - 48, window.innerHeight - 58))}px`;
  toolbar.classList.remove("hidden");
}

function handleDocumentClick(event) {
  const action = event.target?.dataset?.action;
  if (action === "set-locale") {
    setLocale(event.target.dataset.locale);
    return;
  }
  if (action === "pdf-prev") {
    changePdfPage(-1);
    return;
  }
  if (action === "pdf-next") {
    changePdfPage(1);
    return;
  }
  if (action === "pdf-zoom-out") {
    changePdfZoom(-0.2);
    return;
  }
  if (action === "pdf-zoom-in") {
    changePdfZoom(0.2);
    return;
  }
  if (action === "save-selection-note") {
    saveSelectionNote();
    return;
  }
  if (action === "delete-note") {
    deleteNote(event.target.closest("[data-note-id]")?.dataset.noteId);
    return;
  }
  if (action === "clear-notes") {
    clearNotes();
    return;
  }
  if (action === "toggle-notes") {
    toggleNotesPanel();
  }
}

function handleNoteInput(event) {
  if (event.target?.dataset?.action !== "edit-note-comment") return;
  const noteId = event.target.closest("[data-note-id]")?.dataset.noteId;
  const notes = loadNotes();
  const note = notes.find((item) => item.id === noteId);
  if (!note) return;
  note.comment = event.target.value;
  saveNotes(notes);
}

function saveSelectionNote() {
  if (!selectionState) return;
  const notes = loadNotes();
  notes.unshift({
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    text: selectionState.text,
    comment: "",
    moduleTitle: selectionState.moduleTitle,
    sourceTitle: selectionState.sourceTitle,
    route: selectionState.route,
    createdAt: new Date().toISOString()
  });
  saveNotes(notes);
  hideSelectionToolbar();
  window.getSelection()?.removeAllRanges();
  renderNotes();
  if (window.matchMedia("(max-width: 1100px)").matches || $(".notes-panel--floating")) {
    setNotesPanelOpen(true);
  }
}

function renderNotes() {
  const target = $("#notesList");
  if (!target) return;
  const notes = loadNotes();
  const countBadge = $("#notesCountBadge");
  if (countBadge) countBadge.textContent = `${notes.length} ${t("notesCount")}`;
  if (!notes.length) {
    target.innerHTML = `<div class="empty-notes">${t("noNotes")}</div>`;
    return;
  }
  target.innerHTML = `
    <div class="notes-tools">
      <strong>${notes.length} ${t("researchNotes")}</strong>
      <button type="button" data-action="clear-notes">${t("clear")}</button>
    </div>
    ${notes.map(noteItem).join("")}
  `;
}

function noteItem(note) {
  return `
    <article class="note-item" data-note-id="${escapeHtml(note.id)}">
      <div class="note-meta">
        <strong>${escapeHtml(note.sourceTitle)}</strong>
        <time>${new Date(note.createdAt).toLocaleString(locale, { hour12: false })}</time>
      </div>
      <blockquote>${escapeHtml(note.text)}</blockquote>
      <label>
        ${t("noteComment")}
        <textarea data-action="edit-note-comment" placeholder="${t("notePlaceholder")}">${escapeHtml(note.comment || "")}</textarea>
      </label>
      <div class="note-actions">
        <a href="${escapeHtml(note.route)}">${t("returnToSource")}</a>
        <button type="button" data-action="delete-note">${t("delete")}</button>
      </div>
    </article>
  `;
}

function loadNotes() {
  try {
    const parsed = JSON.parse(localStorage.getItem(NOTES_KEY) || "[]");
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function saveNotes(notes) {
  localStorage.setItem(NOTES_KEY, JSON.stringify(notes.slice(0, 200)));
}

function deleteNote(noteId) {
  if (!noteId) return;
  saveNotes(loadNotes().filter((note) => note.id !== noteId));
  renderNotes();
}

function clearNotes() {
  saveNotes([]);
  renderNotes();
}

function toggleNotesPanel() {
  const panel = $(".notes-panel");
  if (!panel) return;
  setNotesPanelOpen(!panel.classList.contains("is-open"));
}

function setNotesPanelOpen(open) {
  const panel = $(".notes-panel");
  const toggle = panel?.querySelector("[data-action='toggle-notes']");
  if (!panel || !toggle) return;
  panel.classList.toggle("is-open", open);
  toggle.setAttribute("aria-expanded", String(open));
}

function hideSelectionToolbar() {
  const toolbar = $("#selectionToolbar");
  toolbar?.classList.add("hidden");
  selectionState = null;
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

export { render };
