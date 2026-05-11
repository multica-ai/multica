## Context

Currently, issues can only be created one at a time via the "New Issue" modal in the workspace UI. There is no API or UI path to create multiple issues in a single operation. Teams who want to seed a project, import a backlog from another tool, or quickly plan a sprint must create each issue manually.

The backend uses sqlc-generated queries over PostgreSQL. The frontend is feature-based React with Zustand stores.

## Goals / Non-Goals

**Goals:**
- Add `POST /workspaces/:id/issues/bulk` to create up to 100 issues in one request
- Add a workspace UI entry point (modal) for bulk import supporting plain-text and CSV
- Validate input client-side before sending; surface per-row errors clearly
- All created issues are workspace-scoped and respect existing auth/multi-tenancy rules

**Non-Goals:**
- File upload (drag-and-drop file picker) — users paste content instead
- Updating or deduplicating existing issues
- Assigning agents or members during bulk import
- Importing attachments or comments

## Decisions

### 1. Single new REST endpoint (`/bulk`) vs. client-side loop

**Decision:** Add a dedicated `POST /workspaces/:id/issues/bulk` endpoint.

**Rationale:** A single round-trip is more efficient and allows the server to wrap all inserts in one transaction. Looping N individual `POST /issues` calls from the client would be slow, generate N WebSocket broadcasts, and offer no atomicity.

**Alternative considered:** Client-side loop — rejected due to latency, partial-failure complexity, and chatty WS events.

### 2. Partial success vs. all-or-nothing

**Decision:** All-or-nothing (single DB transaction). If any row fails validation server-side, the entire request is rejected with a 422 and per-row error details.

**Rationale:** Simpler mental model for users. Partial imports leave ambiguous state. Client-side pre-validation should catch most errors before submission.

### 3. CSV parsing location

**Decision:** Parse CSV in the browser (client-side) before sending a structured JSON payload to the API.

**Rationale:** Keeps the API simple (always receives `[]IssueCreateRequest`). Avoids multipart file handling. Errors are surfaced immediately without a server round-trip.

### 4. Import UI entry point

**Decision:** New "Import Issues" button in the issues list toolbar, opening a modal (not a separate page).

**Rationale:** Consistent with existing quick-action patterns. A modal keeps context and avoids navigation. The import flow is short enough to fit in a modal.

### 5. Batch size limit

**Decision:** Cap at 100 issues per request server-side.

**Rationale:** Reasonable upper bound for a team planning workflow. Prevents abuse. Can be raised later without a breaking change.

## Risks / Trade-offs

- **WS broadcast on bulk insert**: Broadcasting one event per issue may flood connected clients. → Mitigation: Broadcast a single `issues.bulk_created` event with an array of created issues; update the frontend store in one batch operation.
- **CSV ambiguity**: Users may paste CSV with different delimiters or extra columns. → Mitigation: Accept comma-delimited only; show a format hint and example in the UI; skip extra columns gracefully.
- **Transaction size**: Inserting 100 rows in one transaction is trivially fast on PostgreSQL with pgvector; no performance concern at this scale.

## Migration Plan

- No database schema changes required.
- Deploy backend with new endpoint; frontend change is additive UI only.
- No rollback complexity — removing the endpoint or modal has no side effects on existing data.

## Open Questions

- Should bulk-created issues trigger individual inbox notifications for workspace members, or be batched into a single notification? (Recommend: skip notifications for bulk imports to avoid inbox flooding — can be revisited.)
