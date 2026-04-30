## 1. Backend — SQL Query

- [x] 1.1 Add `BulkCreateIssues` SQL query to `server/pkg/db/queries/issue.sql` (insert multiple rows, return all)
- [x] 1.2 Run `make sqlc` to regenerate `server/pkg/db/generated/` code

## 2. Backend — API Handler

- [x] 2.1 Define `BulkCreateIssuesRequest` and `BulkCreateIssueItem` request structs in `server/internal/handler/issue.go`
- [x] 2.2 Implement `BulkCreateIssues` handler: parse body, validate (non-empty titles, max 100), insert in transaction, broadcast `issues.bulk_created` WS event, return 201
- [x] 2.3 Register `POST /workspaces/{workspaceId}/issues/bulk` route in `cmd/server/router.go` (protected, requires JWT)
- [x] 2.4 Write Go unit tests for the handler covering success, empty title, and over-limit cases

## 3. Frontend — API Client

- [x] 3.1 Add `bulkCreateIssues(workspaceId, items)` method to `apps/workspace/src/shared/api/` API client
- [x] 3.2 Add `issues.bulk_created` WS event handler in `features/realtime/` that batch-upserts issues into the issue store

## 4. Frontend — Import Modal

- [x] 4.1 Create `apps/workspace/src/features/issues/components/BulkImportModal.tsx` with Text/CSV tab switcher
- [x] 4.2 Implement Text mode parser: split on newlines, trim, filter empty lines → preview table
- [x] 4.3 Implement CSV mode parser: parse header row + data rows, map columns (title, description, priority, status) → preview table
- [x] 4.4 Add live preview table component showing parsed rows with row count
- [x] 4.5 Disable Import button when no valid rows; show inline hint when input is all whitespace
- [x] 4.6 On submit, call `bulkCreateIssues`, close modal on success, display server errors on failure
- [x] 4.7 Add "Import Issues" button to the issues list toolbar that opens the modal

## 5. Tests

- [x] 5.1 Write Vitest unit tests for the Text and CSV parsers
- [x] 5.2 Write Vitest unit tests for `BulkImportModal` covering empty input, valid text, valid CSV, and error states
- [x] 5.3 Add E2E test in `e2e/` for the bulk import flow (text mode: paste titles, verify issues created)

## 6. Verification

- [x] 6.1 Run `make check` and confirm all TypeScript, Go, and E2E checks pass
