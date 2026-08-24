# 贡献指南

[English](CONTRIBUTING.md) | [简体中文](CONTRIBUTING.zh-CN.md)

感谢你帮助完善 AgentCraft。项目欢迎四类贡献：论文解读、事实修正、工程实验、网站体验。

## 开始之前

1. 先搜索现有 Issue 和 `ai-agent-roadmap-site/src/data.js`，确认内容没有重复。
2. 一个 Pull Request 尽量只解决一个主题。
3. 不要提交论文 PDF、未经授权的图片、API Key、个人数据或模型生成的未核验引用。
4. 新增事实应优先引用论文原文、作者项目页、官方代码仓库等一手来源。
5. 默认文档使用英语；用户可见含义变化时，在同一个 Pull Request 中同步对应的 `.zh-CN.md`。

## 新增论文

中文论文条目位于 `ai-agent-roadmap-site/src/data.js`，英文内容位于 `ai-agent-roadmap-site/src/data.en.js`。每条至少包含：

```js
{
  title: "论文标题",
  url: "论文或作者项目页",
  tags: ["主题一", "主题二"],
  overview: "可独立阅读的概要",
  explanation: "按直觉、方法与证据、Agent 意义、局限与落地分成四段的原创解读"
}
```

解读必须覆盖问题背景、核心方法、Agent 意义和工程启发。写作模板见 `docs/paper-reading-template.md`。请同时在 `assets/papers/` 增加对应的原创概念图，并确保你拥有发布权。

## 新增工程实验

- 实验应能在干净环境中运行，并提供清晰的 `README.md`。
- 保留确定性、无需 API Key 的 `DemoModel` 回归路径。
- 工具调用必须有参数校验、错误处理、超时和最小权限说明。
- 至少提供一个成功路径测试和一个失败路径测试。
- Session、Goal、Plan、Memory、Metrics 与 Trajectory 是不同状态所有者，修改 SQLite schema 时要说明迁移方式。
- 不要把复杂框架当作概念本身，先解释它解决了哪个 Agent 系统问题。

## 本地检查

```bash
npm test
npm run papers:verify
make open-source-check
```

提交信息建议使用 `docs: ...`、`feat: ...`、`fix: ...`、`test: ...` 等清晰前缀。Pull Request 请说明改动动机、验证方式和新增来源。
