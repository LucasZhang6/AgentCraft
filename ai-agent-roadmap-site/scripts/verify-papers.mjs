import { open, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { downloadablePaperAssets, paperLibrary } from "../src/paper-library.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outputDir = path.join(root, "assets/pdfs");
const failures = [];
let totalBytes = 0;

for (const paper of downloadablePaperAssets) {
  const filePath = path.join(outputDir, paper.file);
  try {
    const info = await stat(filePath);
    const handle = await open(filePath, "r");
    const header = Buffer.alloc(5);
    try {
      await handle.read(header, 0, header.length, 0);
    } finally {
      await handle.close();
    }
    if (info.size < 10_000 || header.toString("ascii") !== "%PDF-") {
      failures.push(`${paper.file}: invalid PDF`);
    } else {
      totalBytes += info.size;
    }
  } catch (error) {
    failures.push(`${paper.file}: ${error.code || error.message}`);
  }
}

if (paperLibrary.length !== 31) failures.push(`expected 31 entries, got ${paperLibrary.length}`);
if (failures.length) {
  console.error("Local paper verification failed:");
  failures.forEach((failure) => console.error(`- ${failure}`));
  process.exit(1);
}

console.log(
  `Verified ${downloadablePaperAssets.length} local PDFs for ${paperLibrary.length} entries (${(
    totalBytes /
    1024 /
    1024
  ).toFixed(1)} MiB).`
);
