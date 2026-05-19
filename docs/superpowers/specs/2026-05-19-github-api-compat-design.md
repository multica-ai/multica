# GitHub API Response Compatibility Design

Date: 2026-05-19
Status: Ready for user review

## Summary

Multica's GitHub integration already covers installation linking, PR auto-linking, PR status display, check-suite aggregation, and auto-advancing issues when merged PRs resolve. The remaining reliability gap is on the frontend API boundary: the GitHub endpoints in `packages/core/api/client.ts` still return typed results directly from `fetch`, rather than parsing unknown JSON with `parseWithFallback`.

This design closes that gap for:

- `GET /api/workspaces/:id/github/connect`
- `GET /api/workspaces/:id/github/installations`
- `GET /api/issues/:id/pull-requests`

The goal is narrow: a malformed or drifted GitHub response should degrade to empty or disabled UI, never white-screen the web app or an installed desktop build.

## Goals

- Parse GitHub API responses at the `packages/core/api` boundary using zod schemas and `parseWithFallback`.
- Keep enum drift non-fatal for PR state, mergeability, and check conclusions.
- Preserve existing UI behavior for valid responses.
- Add regression tests for malformed GitHub responses.
- Avoid backend changes unless implementation discovers an actual contract mismatch.

## Non-Goals

- Changing GitHub webhook behavior.
- Adding manual PR linking or unlinking.
- Changing the PR card layout.
- Adding GitHub App authentication improvements.
- Expanding CI/check support beyond the existing `check_suite` aggregation.

## Current State

The GitHub integration has backend handlers in `server/internal/handler/github.go`. The issue detail page renders linked PRs through `PullRequestList`, and realtime invalidation already refreshes all PR queries on `pull_request:*` websocket events.

The frontend API methods currently return typed values directly:

- `getGitHubConnectURL(workspaceId): Promise<GitHubConnectResponse>`
- `listGitHubInstallations(workspaceId): Promise<ListGitHubInstallationsResponse>`
- `listIssuePullRequests(issueId): Promise<{ pull_requests: GitHubPullRequest[] }>`

That is weaker than the repository's installed-desktop compatibility rule. If a newer backend changes or omits a field, older desktop builds can crash in UI code that assumes arrays or strings exist.

## Design

### Schema Placement

Add GitHub schemas to `packages/core/api/schemas.ts`, alongside the existing high-risk response schemas.

Schemas should be lenient:

- Use `z.string()` for server-driven enum-like fields.
- Default arrays to `[]`.
- Default numeric counts to `0`.
- Allow `null` for nullable fields.
- Use `.loose()` so future fields pass through.

### Response Fallbacks

Add fallback constants:

- `EMPTY_GITHUB_CONNECT_RESPONSE`
  - `{ configured: false }`
- `EMPTY_GITHUB_INSTALLATIONS_RESPONSE`
  - `{ configured: false, installations: [] }`
- `EMPTY_GITHUB_PULL_REQUESTS_RESPONSE`
  - `{ pull_requests: [] }`

Fallback behavior is intentionally conservative:

- If connect config response is malformed, the UI behaves as if GitHub is not configured.
- If installation list response is malformed, settings shows no installations and disables or hides dependent affordances.
- If PR list response is malformed, issue detail shows "No linked pull requests yet" instead of crashing.

### Endpoint Parsing

Change the three API methods in `packages/core/api/client.ts` to:

1. Fetch as `unknown`.
2. Parse with the relevant schema.
3. Return fallback on validation failure.

`deleteGitHubInstallation` stays unchanged because it has no response body to parse.

### Type Compatibility

The existing TypeScript interfaces can remain strict enough for UI code, but the parser should accept broader input. In particular:

- `GitHubPullRequest.state` may be a future string even though UI currently knows `open`, `draft`, `merged`, and `closed`.
- `checks_conclusion` may be a future string or missing.
- `mergeable_state` is already modeled as `string`.

The UI already has fallback behavior in `PullRequestList`:

- Unknown PR state falls back to a generic PR icon and raw state label.
- Unknown status kind becomes "unknown".
- Missing check counts behave as zero.

Implementation should avoid making schemas stricter than those UI fallbacks.

## Data Flow

### GitHub Connect

1. Settings Integrations tab calls `api.getGitHubConnectURL(wsId)`.
2. API client parses `GET /api/workspaces/:id/github/connect`.
3. Valid response opens GitHub install URL.
4. Invalid response returns `configured: false`; UI shows the existing not-configured path.

### Installations

1. Settings Integrations tab calls `api.listGitHubInstallations(wsId)` through React Query.
2. API client parses `GET /api/workspaces/:id/github/installations`.
3. Valid response drives connect button state.
4. Invalid response returns an empty list and `configured: false`.

### Pull Requests

1. Issue detail renders `PullRequestList`.
2. React Query calls `api.listIssuePullRequests(issueId)`.
3. API client parses `GET /api/issues/:id/pull-requests`.
4. Valid response renders PR cards.
5. Invalid response returns `pull_requests: []`.
6. `pull_request:*` websocket events continue invalidating the PR query prefix.

## Error Handling

Validation failures should follow the existing `parseWithFallback` behavior:

- Log a warning for diagnostics.
- Return a safe fallback.
- Never throw into React rendering.

Network errors still surface through React Query as before. This design only changes JSON shape validation, not transport failure behavior.

## Testing

Add coverage in `packages/core/api/schema.test.ts`.

Required cases:

- GitHub connect response accepts `{ configured: true, url: "..." }`.
- GitHub connect response falls back when `configured` is the wrong type.
- Installation list accepts valid rows.
- Installation list defaults missing `installations` to `[]`.
- Installation list falls back when `installations` is not an array.
- PR list accepts valid rows with check counts and diff stats.
- PR list defaults missing `pull_requests` to `[]`.
- PR rows tolerate unknown `state`, `mergeable_state`, and `checks_conclusion`.
- PR list falls back when `pull_requests` is not an array.

No browser or backend tests are required for this change unless implementation changes UI behavior or backend response shapes.

## Rollout

This is a low-risk frontend/core-only compatibility hardening. It can ship independently of the AI Skill Finder work.

The expected user-visible behavior is unchanged for healthy responses. The only visible change is safer degradation when the backend response drifts or is malformed.
