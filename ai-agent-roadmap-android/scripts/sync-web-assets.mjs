import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const webRoot = path.resolve(root, "../ai-agent-roadmap-site");
const assetsRoot = path.resolve(root, "src/main/assets");

function ensureInside(child, parent) {
  const relative = path.relative(parent, child);
  if (relative.startsWith("..") || path.isAbsolute(relative)) {
    throw new Error(`Refusing to write outside ${parent}: ${child}`);
  }
}

function withoutExports(source) {
  return source.replace(/^export /gm, "");
}

function localizedData(source, suffix) {
  return source
    .replace(/^import .*?;\n/gm, "")
    .replaceAll("sourceModules", "modulesZh")
    .replace(/^export const modules =/m, `const modules${suffix} =`)
    .replace(/^export const architecture =/m, `const architecture${suffix} =`)
    .replace(/^export const metrics =/m, `const metrics${suffix} =`);
}

async function bundleJavaScript() {
  const [dataZh, dataEn, i18n, paperLibrary, practice, app] = await Promise.all([
    readFile(path.join(webRoot, "src/data.js"), "utf8"),
    readFile(path.join(webRoot, "src/data.en.js"), "utf8"),
    readFile(path.join(webRoot, "src/i18n.js"), "utf8"),
    readFile(path.join(webRoot, "src/paper-library.js"), "utf8"),
    readFile(path.join(webRoot, "src/practice.js"), "utf8"),
    readFile(path.join(webRoot, "src/app.js"), "utf8")
  ]);

  let appBody = app
    .replace(/^import[\s\S]*?;\n/gm, "")
    .replace(/\nexport \{ render \};\s*$/m, "\nwindow.__roadmapRender = render;\n");

  appBody = appBody.replace(
    '  window.scrollTo({ top: 0, behavior: "instant" });',
    '  window.scrollTo({ top: 0, behavior: "instant" });\n  scheduleAndroidProbe();'
  );

  const androidProbe = `
function scheduleAndroidProbe() {
  if (!window.AndroidProbe || typeof window.AndroidProbe.report !== "function") return;
  const emit = () => {
    const images = Array.from(document.images || []);
    const brokenImages = images
      .filter((image) => image.complete && image.naturalWidth === 0)
      .map((image) => image.getAttribute("src") || image.currentSrc || "");
    const payload = {
      hash: window.location.hash || "#/",
      lang: document.documentElement.lang,
      title: document.querySelector("h1")?.textContent || document.title,
      bodyChars: document.body?.innerText?.length || 0,
      moduleCards: document.querySelectorAll(".module-card").length,
      paperCards: document.querySelectorAll(".paper-card").length,
      paperReader: Boolean(document.querySelector(".paper-reader")),
      notesPanel: Boolean(document.querySelector(".notes-panel")),
      languageControls: document.querySelectorAll(".language-control").length,
      images: images.length,
      loadedImages: images.filter((image) => image.complete && image.naturalWidth > 0).length,
      brokenImages,
      timestamp: Date.now()
    };
    window.AndroidProbe.report(JSON.stringify(payload));
  };
  [300, 1200, 3000, 6000].forEach((delay) => window.setTimeout(emit, delay));
}
`;

  return [
    '"use strict";',
    localizedData(dataZh, "Zh"),
    localizedData(dataEn, "En"),
    withoutExports(i18n),
    withoutExports(paperLibrary),
    withoutExports(practice).replace(/^const practiceProject = chinese;\n?/m, ""),
    androidProbe,
    appBody
  ].join("\n\n");
}

async function main() {
  ensureInside(assetsRoot, root);
  await rm(assetsRoot, { recursive: true, force: true });
  await mkdir(path.join(assetsRoot, "assets"), { recursive: true });
  await cp(path.join(webRoot, "assets"), path.join(assetsRoot, "assets"), { recursive: true });
  await cp(path.join(webRoot, "styles.css"), path.join(assetsRoot, "styles.css"));

  const html = await readFile(path.join(webRoot, "index.html"), "utf8");
  const androidHtml = html.replace(
    '<script type="module" src="./src/app.js"></script>',
    '<script src="./app.bundle.js"></script>'
  );
  await writeFile(path.join(assetsRoot, "index.html"), androidHtml);
  await writeFile(path.join(assetsRoot, "app.bundle.js"), await bundleJavaScript());
  console.log(`Synced Android assets to ${assetsRoot}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
