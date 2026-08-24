# Local Paper Cache

[English](README.md) | [简体中文](README.zh-CN.md)

Download the 25 core PDFs used by the site's 31 paper entries plus the one
supplemental Constitutional AI paper:

```bash
npm run papers:download
```

The site can then run offline with `npm run dev`. A paper's main action opens
the structured bilingual analysis; the source action opens the local PDF.js
reader with page navigation, zoom, and download. It does not depend on an
external paper host or the browser's built-in PDF plugin.

PDFs are not committed to Git. This avoids inflating the repository and
preserves the original publishers' distribution and licensing boundaries.
Sources and entry mappings are defined in `src/paper-library.js`.
`download-report.json` records local size, SHA-256, and the source actually used.

Verify the cache:

```bash
npm run papers:verify
```
