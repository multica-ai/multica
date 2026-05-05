import type { ArtifactKind } from "@multica/core/types";

/**
 * Skeleton bodies pre-filled into CreateArtifactSheet when a kind is picked.
 * Light-weight by design: the structure is a hint, not a schema. Authors are
 * free to delete or restructure. Persistence is plain markdown — the typed
 * skeleton lives only at creation time.
 */
export const KIND_TEMPLATES: Record<ArtifactKind, string> = {
  report: `## Summary

(One-paragraph TL;DR.)

## Findings

-

## Recommendation

(What should happen next, and why.)
`,
  plan: `## Goal

(What does done look like?)

## Approach

(High-level steps and rationale.)

## Tasks

- [ ]

## Risks

-
`,
  decision: `## Context

(What forces are in play? Why is a decision needed now?)

## Decision

(What did we decide?)

## Consequences

- Pro:
- Con:
- Trade-off:
`,
  diagram: "```mermaid\nflowchart LR\n  A[Start] --> B[Decision]\n  B -->|yes| C[Outcome]\n  B -->|no| D[Alternative]\n```\n",
  note: "",
};

export const KIND_HELP: Record<ArtifactKind, string> = {
  report: "Investigation results, analysis, or summaries.",
  plan: "Proposed approach with task breakdown and sequencing.",
  decision: "ADR-style: context, decision, consequences.",
  diagram: "Mermaid diagram (flowchart, sequence, ER, etc.).",
  note: "Free-form long-text note that doesn't fit a comment.",
};
