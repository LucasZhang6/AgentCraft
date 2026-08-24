# 本地论文缓存

[English](README.md) | [简体中文](README.zh-CN.md)

运行以下命令，将站点的 31 个论文条目对应的 25 份核心 PDF 和 1 份组合条目补充论文下载到本目录：

```bash
npm run papers:download
```

随后可以断网运行 `npm run dev`。论文详情页的主按钮会进入站内阅读器，通过本地 PDF.js 提供翻页、缩放和下载，不依赖原始论文站点或浏览器内置 PDF 插件。

PDF 文件不提交到 Git：一方面避免显著增加开源仓库体积，另一方面保留各论文原始发布方的分发与许可边界。下载来源和条目映射定义在 `src/paper-library.js`，`download-report.json` 记录当前本地缓存的大小、SHA-256 和实际下载来源。

校验本地缓存：

```bash
npm run papers:verify
```
