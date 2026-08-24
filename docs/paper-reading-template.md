# Paper Review Format

[English](paper-reading-template.md) | [简体中文](paper-reading-template.zh-CN.md)

This format keeps reviews consistent, readable, and verifiable. Separate facts
reported by the paper, claims made by its authors, and engineering inference by
project maintainers.

## Metadata

- Title
- Authors and affiliations
- Publication date and venue
- Primary paper URL
- Official code or project page
- Topic: LLM / Loop / Memory / Tool / Planning / Evaluation / Safety / RL / Engineering

## Overview

Write a concise paragraph that independently answers three questions: what
problem is studied, what central method is used, and why the result matters.
Avoid unexplained terminology and unsupported claims such as "significantly
better."

## Detailed Explanation

Use four readable parts in the site data and expand them with the following
evidence when preparing a long-form document.

### Intuition and Problem

Describe the concrete system failure that exists without the method. Use an
agent-task example when useful, and state the boundary of the research problem.

### Method and Evidence

Explain inputs, important state, decisions, optimization, and outputs. For an
equation, first explain what each variable represents in the system. Identify
datasets or environments, major baselines, metrics, central results, ablations,
cost reporting, and failure examples.

### Agent-System Relevance

Name the component changed by the paper: Controller, Memory, Tool Layer,
Planner, Evaluator, Logger, or Skill Library. Explain both the benefit and the
new cost or attack surface.

### Limits and Implementation

Propose a one- or two-day controlled experiment with inputs, outputs, an
acceptance metric, and a baseline. Then discuss at least three limits, such as
applicability, evaluation gaps, scale/cost, security risk, or reproduction
difficulty. Mark your own judgment as inference rather than a paper conclusion.

## Language Parity

English and Simplified Chinese entries should preserve the same claims,
citations, numbers, and limitations. A translation may choose more natural
examples, but it must not strengthen a result or remove a caveat. Run `npm test`
to validate coverage for both locale datasets.
