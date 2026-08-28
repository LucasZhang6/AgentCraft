# Architecture Image Assets

[English](README.md) | [简体中文](README.zh-CN.md)

This directory contains ImageGen diagrams used by the README and design docs.
They were generated with the Codex `image_gen` tool and copied into the
repository, so the project does not depend on temporary or external image URLs.

| File | Subject | Used by |
| --- | --- | --- |
| `agent-capability-roadmap.png` | capability path from LLM foundations to productization | root README |
| `agent-runtime-architecture.png` | runtime control, loop, and data planes | README and architecture docs |
| `your-agent-pipeline.png` | paper-analysis pipeline | root README |
| `minimal-agent-loop.png` | decision and action loop | README and Your Agent guide |
| `agent-replayable-trajectory.png` | replayable run trace | root README |

## Shared Visual Prompt

```text
Create a clean, high-resolution 16:9 technical infographic for open-source
documentation. Use a white background, strict grid, crisp vector-like lines,
generous spacing, and a restrained charcoal, teal, coral, mustard, and light-gray
palette. Keep every supplied label exact and legible. Use only the stated nodes
and directional connections. Avoid logos, watermarks, 3D, gradients, heavy
shadows, decorative characters, rounded pills, crossed arrows, and extra text.
```

The exact node and label specifications for the existing images are preserved in
the [Simplified Chinese asset guide](README.zh-CN.md). Generated images can
misrender text or connectors; manually inspect every label, arrow direction,
module boundary, and responsive display before accepting a replacement.
