import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const packageRoot = path.join(root, "node_modules/pdfjs-dist");
const outputRoot = path.join(root, "assets/vendor/pdfjs");

async function main() {
  const packageJson = JSON.parse(await readFile(path.join(packageRoot, "package.json"), "utf8"));
  await rm(outputRoot, { recursive: true, force: true });
  await mkdir(outputRoot, { recursive: true });
  await Promise.all([
    cp(path.join(packageRoot, "build/pdf.mjs"), path.join(outputRoot, "pdf.mjs")),
    cp(path.join(packageRoot, "build/pdf.worker.mjs"), path.join(outputRoot, "pdf.worker.mjs")),
    cp(path.join(packageRoot, "standard_fonts"), path.join(outputRoot, "standard_fonts"), { recursive: true }),
    cp(path.join(packageRoot, "cmaps"), path.join(outputRoot, "cmaps"), { recursive: true }),
    cp(path.join(packageRoot, "wasm"), path.join(outputRoot, "wasm"), { recursive: true }),
    cp(path.join(packageRoot, "LICENSE"), path.join(outputRoot, "LICENSE"))
  ]);
  await writeFile(
    path.join(outputRoot, "NOTICE.md"),
    `# PDF.js vendor files\n\nGenerated from \`pdfjs-dist@${packageJson.version}\` under the Apache-2.0 license. Run \`npm run pdfjs:sync\` after updating the dependency.\n`
  );
  console.log(`Synced pdfjs-dist@${packageJson.version} to ${outputRoot}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
