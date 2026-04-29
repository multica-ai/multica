## 1. Database and backend contract

- [ ] 1.1 Add a migration that introduces nullable `start_date` and `end_date` columns on the `issue` table.
- [ ] 1.2 Extend issue SQL queries to read and write `start_date` and `end_date`, then regenerate the sqlc artifacts.
- [ ] 1.3 Update issue request parsing, response serialization, and server-side validation so create/update flows support schedule dates, clearing, and invalid-range rejection.

## 2. Backend eventing and test coverage

- [ ] 2.1 Extend issue update payloads with `start_date_changed`, `end_date_changed`, `prev_start_date`, and `prev_end_date` metadata.
- [ ] 2.2 Update activity and notification listeners so schedule date changes are recorded consistently with existing due-date changes.
- [ ] 2.3 Add backend tests for creating, updating, clearing, and rejecting invalid issue schedule windows.

## 3. apps/workspace issue workflows

- [ ] 3.1 Extend issue/API types and any issue draft state in `apps/workspace` to carry `start_date` and `end_date`.
- [ ] 3.2 Reuse or generalize the existing date-picker UI so create-issue and issue-detail flows in `apps/workspace` can view, set, and clear both schedule dates.
- [ ] 3.3 Add or update `apps/workspace` tests for schedule date editing behavior in issue workflows.

## 4. End-to-end verification

- [x] 4.1 Add or update issue workflow coverage that proves users can create and edit `start_date` and `end_date` through the primary UI.
- [x] 4.2 Verify the final change with the relevant backend, frontend, and workflow tests before archiving the change.
