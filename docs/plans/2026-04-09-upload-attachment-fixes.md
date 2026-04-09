# Upload & Attachment Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 5 interrelated upload/attachment issues discovered during drag-upload development (MUL-410).

**Architecture:** Three backend fixes (content-type sniffing, Content-Disposition, list API optimization) + one frontend fix (decouple description editor uploads from attachment records) + one no-code confirmation (agent file discovery paths). All changes follow existing patterns — no new abstractions.

**Tech Stack:** Go backend (Chi, sqlc, S3), Next.js frontend (TanStack Query, Tiptap editor), PostgreSQL.

---

## Overview

| # | Issue | Type | Files |
|---|-------|------|-------|
| 1 | SVG content-type sniffing | Backend bug | `server/internal/handler/file.go` |
| 2 | Content-Disposition inline vs attachment | Backend bug | `server/internal/storage/s3.go` |
| 3 | Attachment records / editor sync | Frontend fix | `packages/views/issues/components/issue-detail.tsx` |
| 4 | List Issues returns full description | Backend optimization | `server/pkg/db/queries/issue.sql`, `server/internal/handler/issue.go`, `server/pkg/db/generated/issue.sql.go` |
| 5 | Agent file discovery redundancy | No code change | Confirmed by #3 |

---

### Task 1: SVG Content-Type Extension Fallback

**Problem:** `http.DetectContentType()` returns `text/xml` for SVG files. CloudFront serves them with wrong content-type, `<img>` tags can't render.

**Files:**
- Modify: `server/internal/handler/file.go:1-16` (imports), `server/internal/handler/file.go:123` (after sniff)

**Step 1: Add extension-based content-type override map and import**

After line 16 (imports block), add `"strings"` import and a package-level `extContentTypes` map. After line 123 (`contentType := http.DetectContentType(buf[:n])`), add fallback lookup:

```go
// In imports, add "strings"

// After the imports block:
var extContentTypes = map[string]string{
	".svg":  "image/svg+xml",
	".css":  "text/css",
	".js":   "application/javascript",
	".mjs":  "application/javascript",
	".json": "application/json",
	".wasm": "application/wasm",
}

// After line 123 (contentType := http.DetectContentType(buf[:n])):
if ct, ok := extContentTypes[strings.ToLower(path.Ext(header.Filename))]; ok {
	contentType = ct
}
```

**Step 2: Build and verify**

Run: `cd server && go build ./...`
Expected: Clean build, no errors.

**Step 3: Commit**

```bash
git add server/internal/handler/file.go
git commit -m "fix(upload): add extension-based content-type fallback for SVG and other sniff-misdetected types"
```

---

### Task 2: Content-Disposition Inline vs Attachment

**Problem:** All uploads set `Content-Disposition: inline`. Browsers display CSV/PDF inline instead of downloading.

**Files:**
- Modify: `server/internal/storage/s3.go:126-136` (Upload function)

**Step 1: Add disposition logic in Upload function**

Before the `PutObject` call (line 128), determine disposition based on content-type. Images, video, audio, and PDF stay `inline`; everything else becomes `attachment`:

```go
// Add before PutObject call:
func isInlineContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/") ||
		strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "audio/") ||
		ct == "application/pdf"
}

// In Upload(), after sanitizeFilename:
disposition := "attachment"
if isInlineContentType(contentType) {
	disposition = "inline"
}

// Change line 133 from:
ContentDisposition: aws.String(fmt.Sprintf(`inline; filename="%s"`, safe)),
// To:
ContentDisposition: aws.String(fmt.Sprintf(`%s; filename="%s"`, disposition, safe)),
```

**Step 2: Build and verify**

Run: `cd server && go build ./...`
Expected: Clean build.

**Step 3: Commit**

```bash
git add server/internal/storage/s3.go
git commit -m "fix(upload): use Content-Disposition attachment for non-media files"
```

---

### Task 3: Decouple Description Editor Uploads from Attachment Records

**Problem:** Description editor uploads create attachment records linked to the issue. When users delete images from the editor, attachment records become stale. The URL already lives in the markdown — attachment records are redundant for description content.

**Fix:** Description editor uploads should NOT pass `issueId`. Comment/reply uploads continue passing `issueId` (comments are not frequently edited, and agents need attachment records for comment file discovery).

**Files:**
- Modify: `packages/views/issues/components/issue-detail.tsx:339-341`

**Step 1: Remove issueId from description upload**

Change the `handleDescriptionUpload` callback (line 339-341) from:

```typescript
const handleDescriptionUpload = useCallback(
  (file: File) => uploadWithToast(file, { issueId: id }),
  [uploadWithToast, id],
);
```

To:

```typescript
const handleDescriptionUpload = useCallback(
  (file: File) => uploadWithToast(file),
  [uploadWithToast],
);
```

This means description image uploads will still go to S3 and return a URL (which gets embedded in the markdown), but no `attachment` DB record will be linked to the issue. The backend `UploadFile` handler already handles this — when no `issue_id` form field is sent, the attachment record is created without an issue link (or falls back to the no-workspace path for non-workspace uploads, but workspace context is still present via headers so a record IS still created, just without `issue_id` set).

**Step 2: Verify typecheck**

Run: `pnpm typecheck`
Expected: Clean.

**Step 3: Commit**

```bash
git add packages/views/issues/components/issue-detail.tsx
git commit -m "fix(editor): decouple description uploads from attachment records"
```

---

### Task 4: Omit Description from List Issues Response

**Problem:** `GET /api/issues` returns full `description` for every issue. With embedded images, descriptions contain CDN URLs making list payloads large. List pages only show titles.

**Approach:** Change `ListIssues` and `ListOpenIssues` SQL queries to select specific columns (excluding `description`, `acceptance_criteria`, `context_refs`). Regenerate sqlc. Add converter functions for the new row types. Frontend already handles `null` description gracefully.

**Files:**
- Modify: `server/pkg/db/queries/issue.sql` (lines 1-8, 60-66)
- Regenerate: `server/pkg/db/generated/issue.sql.go`
- Modify: `server/internal/handler/issue.go` (add converters, update ListIssues handler)

**Step 1: Update SQL queries**

Change `ListIssues` (lines 1-8) from `SELECT *` to explicit columns:

```sql
-- name: ListIssues :many
SELECT id, workspace_id, title, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, due_date, created_at, updated_at, number, project_id
FROM issue
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
ORDER BY position ASC, created_at DESC
LIMIT $2 OFFSET $3;
```

Change `ListOpenIssues` (lines 60-66) similarly:

```sql
-- name: ListOpenIssues :many
SELECT id, workspace_id, title, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, due_date, created_at, updated_at, number, project_id
FROM issue
WHERE workspace_id = $1
  AND status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
ORDER BY position ASC, created_at DESC;
```

**Step 2: Regenerate sqlc**

Run: `make sqlc`

This will generate `ListIssuesRow` and `ListOpenIssuesRow` types without `Description`, `AcceptanceCriteria`, `ContextRefs`.

**Step 3: Add converter functions in issue.go**

After `issueToResponse` (line 66), add two new converters for the list row types:

```go
func issueListRowToResponse(i db.ListIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}

func openIssueRowToResponse(i db.ListOpenIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}
```

**Step 4: Update ListIssues handler**

In `ListIssues` handler:
- Line 257: change `issueToResponse(issue, prefix)` → `openIssueRowToResponse(issue, prefix)`
- Line 312: change `issueToResponse(issue, prefix)` → `issueListRowToResponse(issue, prefix)`

**Step 5: Build and verify**

Run: `cd server && go build ./...`
Expected: Clean build.

**Frontend impact (no changes needed):**
- Board card (board-card.tsx:61): `storeProperties.description && issue.description` — short-circuits on `null`, won't render description. Correct behavior.
- Issue detail (issue-detail.tsx:210): `initialData: () => allIssues.find(...)` — the seeded issue will have `null` description, but the detail query fetches full issue with description. Brief loading state is acceptable.

**Step 6: Commit**

```bash
git add server/pkg/db/queries/issue.sql server/pkg/db/generated/issue.sql.go server/internal/handler/issue.go
git commit -m "perf(api): omit description from list issues response to reduce payload size"
```

---

### Task 5: Confirm Agent File Discovery (No Code Change)

**Confirmation:** With Task 3 implemented:
- **Description files:** Agent reads issue description markdown → finds CDN URLs directly. No attachment record needed.
- **Comment files:** Agent uses `GET /api/issues/{id}` → `attachments` array for issue-linked files, plus comment content markdown URLs.
- **CLI attachment download:** `multica attachment download <id>` works for files that DO have attachment records (comment uploads).
- **No redundancy:** Two paths serve different purposes — markdown URLs for inline content, attachment records for standalone files.

No code change required. This task is resolved by Task 3.

---

### Task 6: Run Full Verification

**Step 1: Run all checks**

```bash
make check
```

This runs: typecheck → TS tests → Go tests → E2E.

**Step 2: Fix any failures and re-run**

**Step 3: Final commit if any fixes needed**

---

## Execution Order

Tasks 1, 2, 3 are independent — can be parallelized.
Task 4 depends on sqlc regeneration.
Task 5 is confirmation only.
Task 6 runs after all code changes.
