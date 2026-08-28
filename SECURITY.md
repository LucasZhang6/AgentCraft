# Security Policy

[English](SECURITY.md) | [简体中文](SECURITY.zh-CN.md)

Your Agent includes real file, shell, web, plugin, MCP, memory, and model
integration code. Its policy boundaries are designed to be inspectable, but the
project is a reference implementation and is not a production security
certification.

## Reporting a Vulnerability

Use [GitHub private vulnerability reporting](https://github.com/LucasZhang6/AgentCraft/security/advisories/new).
Do not open a public issue containing exploitable details. Include the affected
files or versions, reproduction conditions, likely impact, and a suggested
mitigation when possible. Maintainers will coordinate disclosure after
confirming the report.

## Automated Checks

Every push and pull request runs CodeQL for Go and JavaScript, `govulncheck`,
both npm audits, and a Trivy filesystem scan covering vulnerabilities, secrets,
and configuration errors. The scheduled scan clears Trivy's local cache and
downloads the current complete vulnerability database before analysis. Each
security run publishes SARIF plus an SPDX JSON SBOM; tagged releases include a
checksummed and keyless-signed SBOM. All third-party GitHub Actions are pinned
to immutable commit SHAs and Dependabot proposes reviewed updates.

## Runtime Boundaries

- API keys are read from environment variables and must not be committed,
  printed, stored in memory records, or included in traces.
- File tools are constrained to the configured workspace and reject path and
  symbolic-link escape attempts.
- Web tools reject loopback, private, and link-local destinations and validate
  redirects. This is a defense layer, not a substitute for network isolation.
- Tools have read, write, or dangerous risk levels. Write and dangerous actions
  require approval by default; unattended `ask` mode fails closed.
- Plugins and MCP servers execute local commands and default to dangerous. Only
  register binaries and servers you trust.
- Web pages, retrieved documents, tool output, shared memory, and quoted chat
  content are untrusted input and cannot grant permission.
- Session and trajectory databases may contain sensitive work content. Protect
  `.agent-data/`, define retention, and remove it before sharing diagnostics.
- `approval=allow` and Feishu auto-approval are intended only for controlled
  automation environments with an external sandbox and narrow credentials.

## Deployment Notes

Bind the HTTP server to loopback unless it is placed behind authenticated TLS.
Set `YOUR_AGENT_ACCESS_ID` when another process connects to the API. Keep the
Feishu sidecar separate from LLM credentials, verify callback tokens, and apply
rate limits at the public ingress. Reassess filesystem, process, network, and
tenant isolation before granting the agent access to private or production
systems.
