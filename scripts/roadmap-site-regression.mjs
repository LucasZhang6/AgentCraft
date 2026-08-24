import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";

const baseURL = process.env.ROADMAP_SITE_URL || "http://127.0.0.1:4174";
const outputDir = path.join("artifacts", "browser");
await mkdir(outputDir, { recursive: true });

const browser = await chromium.launch({ headless: true });
try {
  for (const viewport of [
    { name: "desktop", width: 1440, height: 900 },
    { name: "mobile", width: 390, height: 844 }
  ]) {
    const page = await browser.newPage({ viewport });
    const errors = [];
    page.on("console", (message) => {
      if (message.type() === "error") errors.push(message.text());
    });
    page.on("pageerror", (error) => errors.push(error.message));

    await page.goto(`${baseURL}/?lang=en#/`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "Eight core topics in an Agent system", exact: true }).waitFor();
    if ((await page.locator(".module-card").count()) !== 8) throw new Error(`${viewport.name}: expected 8 modules`);
    if ((await page.locator("html").getAttribute("lang")) !== "en") throw new Error(`${viewport.name}: site did not default to English`);
    await assertNoOverflow(page, `${viewport.name} English home`);

    await page.getByRole("button", { name: "简体中文", exact: true }).click();
    await page.getByRole("heading", { name: "Agent 系统的八个核心主题", exact: true }).waitFor();
    if ((await page.locator("html").getAttribute("lang")) !== "zh-CN") throw new Error(`${viewport.name}: Chinese locale was not applied`);

    await page.getByRole("link", { name: "参考实现", exact: true }).click();
    await page.getByRole("heading", { name: /参考实现: Paper Agent/ }).waitFor();
    if ((await page.locator(".phase-card").count()) !== 7) throw new Error(`${viewport.name}: expected 7 implementation layers`);
    const chinesePractice = await page.locator("main").innerText();
    for (const token of ["Prompt Cache", "MCP", "awaiting_acceptance", "L3"]) {
      if (!chinesePractice.includes(token)) throw new Error(`${viewport.name}: Chinese practice view missing ${token}`);
    }

    await page.getByRole("button", { name: "English", exact: true }).click();
    await page.getByRole("heading", { name: /Reference runtime: Paper Agent/ }).waitFor();
    if (new URL(page.url()).hash !== "#/practice/timeline") throw new Error(`${viewport.name}: locale switch lost the current route`);
    await assertNoOverflow(page, `${viewport.name} English practice`);

    await page.goto(`${baseURL}/?lang=en#/paper/llm-foundation/0`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", { name: "Detailed explanation", exact: true }).waitFor();
    if ((await page.locator(".paper-explanation-part").count()) !== 4) throw new Error(`${viewport.name}: paper explanation should have four parts`);
    if ((await page.locator(".source-actions a").count()) < 1) throw new Error(`${viewport.name}: paper reader has no local PDF action`);
    await assertNoOverflow(page, `${viewport.name} paper reader`);

    await page.screenshot({
      path: path.join(outputDir, `roadmap-site-${viewport.name}.png`),
      fullPage: false
    });
    if (errors.length) throw new Error(`${viewport.name}: browser errors: ${errors.join(" | ")}`);
    await page.close();
  }
  console.log("AI Agent Roadmap bilingual browser regression passed");
} finally {
  await browser.close();
}

async function assertNoOverflow(page, label) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 2
  );
  if (overflow) throw new Error(`${label}: horizontal overflow detected`);
}
