# @multica/cerebro-references

Cerebro fork "issue references" extension: typed pointers from a Multica
issue to an external or internal object (GitHub PRs, Stripe customers,
Linear issues, internal entities). The backend is the
`server/internal/cerebro/references` package; this frontend package
exposes the API surface to React consumers.

## Owns

- Type definitions for the wire shape (`IssueReference`).
- Query keys + TanStack Query hooks (`useIssueReferences`,
  `useAddReference`, `useUpdateReference`, `useDeleteReference`).
- Per-object renderer registry (`registerObjectRenderer`,
  `getObjectRenderer`, `listRegisteredObjects`) with a fallback that
  shows raw `ref_id` for unknown object kinds.
- Built-in renderers (currently: `github_pr`).

## May land here

- New built-in renderers (`stripe_customer`, `linear_issue`, …).
- Helper utilities for parsing user-entered identifiers and building
  reference metadata.

## May NOT land here

- UI components (cards, lists, dialogs). Those live in
  `@multica/cerebro-references/views` once the issue-page mount lands —
  this core layer stays headless and bundler-friendly.

## Imports

- May import from: `@multica/core`, `@multica/cerebro-feature-flags`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`,
  `@multica/ui`, `@multica/views`.
