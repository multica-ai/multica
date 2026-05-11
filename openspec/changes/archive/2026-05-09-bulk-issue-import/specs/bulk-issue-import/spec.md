## ADDED Requirements

### Requirement: Bulk issue creation API endpoint

The system SHALL provide a `POST /workspaces/:workspaceId/issues/bulk` endpoint that accepts an array of issue creation payloads and creates all issues atomically within a single database transaction.

- The endpoint SHALL require valid JWT authentication and workspace membership.
- The endpoint SHALL reject requests with more than 100 issues with a 422 response.
- The endpoint SHALL reject requests where any issue has an empty or whitespace-only title with a 422 response, including per-row error details.
- The endpoint SHALL create all issues within a single transaction; if any insert fails, no issues SHALL be created.
- On success, the endpoint SHALL return 201 with the array of created issues.
- The endpoint SHALL broadcast a single `issues.bulk_created` WebSocket event containing all created issues.

#### Scenario: Successful bulk creation

- **WHEN** an authenticated workspace member sends a POST to `/workspaces/:id/issues/bulk` with a valid array of 1–100 issue objects
- **THEN** all issues are created and returned with 201, and a single `issues.bulk_created` WS event is broadcast

#### Scenario: Exceeds maximum batch size

- **WHEN** the request payload contains more than 100 issue objects
- **THEN** the server responds with 422 and an error message indicating the limit was exceeded

#### Scenario: Invalid row — empty title

- **WHEN** one or more issue objects in the payload have an empty or whitespace-only title
- **THEN** the server responds with 422, no issues are created, and the response includes which rows failed and why

#### Scenario: Unauthenticated request

- **WHEN** the request is made without a valid JWT
- **THEN** the server responds with 401 and no issues are created

### Requirement: Bulk import UI modal

The workspace app SHALL provide an "Import Issues" entry point in the issues list view that opens a modal allowing users to paste plain-text or CSV content to create multiple issues at once.

- The modal SHALL offer two input modes: **Text** (one title per line) and **CSV** (columns: title, description, priority, status).
- In Text mode, each non-empty line SHALL become one issue title; empty lines SHALL be ignored.
- In CSV mode, the first row SHALL be treated as a header row; accepted column names are `title`, `description`, `priority`, `status` (case-insensitive).
- The modal SHALL display a live preview table of parsed issues before the user submits.
- The modal SHALL validate that at least one valid issue exists before enabling the Import button.
- After a successful import, the modal SHALL close and the issues list SHALL reflect the newly created issues.
- After a failed import, the modal SHALL display the server-returned error details without closing.

#### Scenario: Import via plain text

- **WHEN** the user selects Text mode, pastes titles (one per line), and clicks Import
- **THEN** issues are created with those titles and default priority/status, the modal closes, and the new issues appear in the list

#### Scenario: Import via CSV with all columns

- **WHEN** the user selects CSV mode, pastes a CSV with header row and data rows including title, description, priority, and status
- **THEN** a preview table is shown, and on confirm, issues are created with the specified fields

#### Scenario: CSV missing optional columns

- **WHEN** the user pastes CSV that includes only a title column
- **THEN** the preview shows only titles, and import proceeds with defaults for missing fields

#### Scenario: All lines are empty

- **WHEN** the user pastes only blank lines or whitespace in Text mode
- **THEN** the Import button remains disabled and a hint is shown indicating no valid issues were found

#### Scenario: Server returns validation error

- **WHEN** the server rejects the bulk request with a 422 error
- **THEN** the modal remains open and the error details are displayed to the user
