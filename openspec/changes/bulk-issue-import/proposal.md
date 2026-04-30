## Why

Users cannot quickly create multiple issues at once — every issue requires a separate form submission. This blocks workflows where teams need to import a backlog, migrate tasks from another tool, or bootstrap a new project with many pre-planned issues.

## What Changes

- Add a **bulk import UI** in the workspace app that accepts plain-text (one title per line) or CSV (title, description, priority, status) input
- Add a **bulk create API endpoint** (`POST /workspaces/:id/issues/bulk`) that accepts an array of issue creation payloads
- Support two import modes in the UI:
  - **Quick text mode**: one issue title per line, creates issues with defaults
  - **CSV mode**: structured columns (title, description, priority, status), with inline preview and validation before submit
- Show a progress/summary after import (e.g., "12 issues created, 1 skipped due to empty title")

## Capabilities

### New Capabilities

- `bulk-issue-import`: Import multiple issues from plain-text or CSV input via the workspace UI and a new bulk-create REST API endpoint

### Modified Capabilities

<!-- No existing spec-level behavior changes -->

## Impact

- **Backend**: New handler and SQL query for bulk issue creation (`server/internal/handler/issue.go`, new sqlc query)
- **Frontend**: New modal/page in `features/issues/` for the import UI
- **API**: New `POST /workspaces/:id/issues/bulk` endpoint
- **Database**: No schema changes; reuses existing `issues` table
- **No breaking changes**
