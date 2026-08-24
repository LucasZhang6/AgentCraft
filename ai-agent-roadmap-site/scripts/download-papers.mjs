import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, open, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fileURLToPath } from "node:url";
import { downloadablePaperAssets } from "../src/paper-library.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = path.join(root, "assets/pdfs");
const force = process.argv.includes("--force");
const concurrency = 3;

async function isPdf(filePath) {
  try {
    const info = await stat(filePath);
    if (info.size < 10_000) return false;
    const handle = await open(filePath, "r");
    try {
      const header = Buffer.alloc(5);
      await handle.read(header, 0, header.length, 0);
      return header.toString("ascii") === "%PDF-";
    } finally {
      await handle.close();
    }
  } catch {
    return false;
  }
}

async function sha256(filePath) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return hash.digest("hex");
}

async function fetchPdf(url, destination) {
  const tempPath = `${destination}.part`;
  await rm(tempPath, { force: true });
  const response = await fetch(url, {
    redirect: "follow",
    headers: {
      accept: "application/pdf,*/*;q=0.8",
      "user-agent": "AI-Agent-Roadmap/0.1 (local research cache)"
    },
    signal: AbortSignal.timeout(120_000)
  });
  if (!response.ok || !response.body) {
    throw new Error(`HTTP ${response.status}`);
  }
  await pipeline(Readable.fromWeb(response.body), createWriteStream(tempPath));
  if (!(await isPdf(tempPath))) {
    const preview = (await readFile(tempPath)).subarray(0, 80).toString("utf8").replace(/\s+/g, " ");
    await rm(tempPath, { force: true });
    throw new Error(`response is not a PDF: ${preview}`);
  }
  await rm(destination, { force: true });
  await rename(tempPath, destination);
}

async function ensurePaper(paper) {
  const destination = path.join(outputDir, paper.file);
  if (!force && (await isPdf(destination))) {
    const info = await stat(destination);
    return { ...paper, status: "cached", size: info.size, sha256: await sha256(destination) };
  }

  const errors = [];
  for (const source of paper.sources) {
    try {
      await fetchPdf(source, destination);
      const info = await stat(destination);
      return {
        ...paper,
        status: "downloaded",
        source,
        size: info.size,
        sha256: await sha256(destination)
      };
    } catch (error) {
      errors.push(`${source}: ${error.message}`);
    }
  }
  throw new Error(`${paper.title}: ${errors.join(" | ")}`);
}

async function main() {
  await mkdir(outputDir, { recursive: true });
  const queue = [...downloadablePaperAssets];
  const results = [];
  const failures = [];

  async function worker() {
    while (queue.length) {
      const paper = queue.shift();
      try {
        const result = await ensurePaper(paper);
        results.push(result);
        console.log(`${result.status.toUpperCase()} ${paper.file} (${Math.round(result.size / 1024)} KiB)`);
      } catch (error) {
        failures.push({ file: paper.file, error: error.message });
        console.error(`FAILED ${error.message}`);
      }
    }
  }

  await Promise.all(Array.from({ length: concurrency }, () => worker()));
  const report = {
    generatedAt: new Date().toISOString(),
    total: downloadablePaperAssets.length,
    completed: results.length,
    failures,
    files: results.sort((a, b) => a.file.localeCompare(b.file))
  };
  await writeFile(path.join(outputDir, "download-report.json"), `${JSON.stringify(report, null, 2)}\n`);

  if (failures.length) {
    throw new Error(`${failures.length} paper downloads failed; see assets/pdfs/download-report.json`);
  }
  const totalBytes = results.reduce((sum, item) => sum + item.size, 0);
  console.log(`Ready: ${results.length} unique PDFs, ${(totalBytes / 1024 / 1024).toFixed(1)} MiB total.`);
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
