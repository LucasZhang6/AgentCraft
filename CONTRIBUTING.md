# Contributing

[English](CONTRIBUTING.md) | [简体中文](CONTRIBUTING.zh-CN.md)

Thank you for improving AgentCraft. Contributions are welcome for paper
analysis, factual corrections, reproducible engineering experiments, runtime
features, evaluation cases, documentation, and accessibility.

## Before You Start

1. Search existing issues and the relevant source file before adding content.
2. Keep a pull request focused on one coherent change.
3. Do not commit paper PDFs, unauthorized images, API keys, personal data, or
   unverified model-generated citations.
4. Prefer primary sources: the paper, an author project page, official
   documentation, or the official code repository.
5. Keep English as the canonical document. Update the matching `.zh-CN.md`
   document in the same pull request when user-facing meaning changes.

The language convention and exceptions for source-language corpora are defined
in [docs/i18n.md](docs/i18n.md).

## Adding a Paper

The Simplified Chinese research data is in
`ai-agent-roadmap-site/src/data.js`; English content is in
`ai-agent-roadmap-site/src/data.en.js`. Every paper must include:

```js
{
  title: "Paper title",
  url: "Primary paper or author project URL",
  tags: ["Topic", "Method"],
  overview: "A concise independent summary",
  explanation: `Intuition: ...

Method and evidence: ...

Agent relevance: ...

Limits and implementation: ...`
}
```

The overview should identify the problem, central method, and main value. The
detailed explanation must cover intuition, method/evidence, agent-system impact,
and engineering limits. Clearly separate paper claims from maintainer
inference. Follow [the paper review format](docs/paper-reading-template.md).

Add or reuse a licensed local PDF mapping in `src/paper-library.js`. Do not
commit downloaded PDFs. Add a publishable visual only when the paper requires a
new entry, then run the content and paper-cache checks.

## Changing Paper Agent

- Keep `DemoModel` deterministic and offline-testable.
- Keep model decisions separate from host authorization.
- Preserve schema validation, workspace/network boundaries, cancellation,
  timeouts, bounded output, and approval semantics.
- Treat Session, Goal, Plan, Memory, Metrics, and Trajectory as different state
  owners; document migrations when their SQLite schemas change.
- Add at least one success test and one failure or denial test.
- Update the English and Chinese runtime guides when commands, defaults, API
  state, persistence, security, or UI behavior changes.

## Local Verification

Run the focused checks while iterating:

```bash
npm test
npm run papers:verify
```

Run the full gate before proposing a release-sensitive change:

```bash
make e2e
make browser-regression
make open-source-check
```

Use clear commit prefixes such as `docs:`, `feat:`, `fix:`, or `test:`. In the
pull request, explain the motivation, behavioral change, verification evidence,
and any new sources or security assumptions.
