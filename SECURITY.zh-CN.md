# 安全策略

[English](SECURITY.md) | [简体中文](SECURITY.zh-CN.md)

本项目包含真实的文件、Shell、网页、插件、MCP、记忆和模型接入代码。示例强调可检查的系统边界，但不代表已经通过生产安全认证。

## 报告安全问题

请通过 [GitHub 私密漏洞报告](https://github.com/LucasZhang6/AgentCraft/security/advisories/new) 提交，不要创建公开 Issue。报告应包含受影响文件、复现条件、潜在影响和建议修复。维护者确认后会协调披露时间。

## 自动化安全检查

每次 Push 和 Pull Request 都会运行 Go/JavaScript CodeQL、`govulncheck`、两组 npm audit，以及覆盖依赖漏洞、Secret 和配置错误的 Trivy 文件系统扫描。定时任务会先清理 Trivy 缓存，再下载当前完整漏洞数据库。安全任务会上传 SARIF 和 SPDX JSON SBOM；Tag 发布会附带校验和 keyless 签名的 SBOM。所有第三方 GitHub Action 都固定到不可变提交 SHA，Dependabot 负责提交可审核的更新。

## 示例代码边界

- API Key 只应通过环境变量提供，不应进入代码、日志、记忆或轨迹。
- 文件工具限制在配置的工作区内，并拒绝路径逃逸和符号链接绕过。
- 网页工具拒绝回环、私网和链路本地地址并检查重定向；它不能替代网络隔离。
- 工具分为只读、写入和危险等级；写入和危险动作默认需要人工审批，非交互 `ask` 模式会安全拒绝。
- 插件和 MCP Server 会运行本地命令，并默认视为危险工具，只能注册可信程序。
- 从网页、RAG、工具输出或共享记忆获得的内容一律视为不可信输入。
- 高风险动作不能仅由模型生成的文本或长期记忆直接授权。
- `.agent-data/` 可能包含敏感工作内容，分享诊断材料前应做好访问控制、保留策略和删除处理。
