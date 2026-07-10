# Members Live Search via dept-sync Design

## Goal

Member search on the members page must run **live against costrict-dept-sync**, at the data source, in real time. Multica must not pull the whole organization into its own process to filter it, and must not depend on a synced snapshot of the directory for search. Only members who have actually been added to a workspace live in Multica's database.

This replaces the earlier snapshot-based search direction: dept-sync now exposes a first-class endpoint, so search moves off any Multica-side import snapshot.

## Background

Today the members-page search box (admin "add member from department directory" in `packages/views/settings/components/members-tab.tsx`) flows:

`browser → multica /api/dept/users/search?q= → deptsync.Client.SearchUsers → (fetch whole dept tree, then GET /department/{id}/users?include_children=true for every department, then filter in Go memory)`

`SearchUsers` ([server/internal/deptsync/client.go:177](../../../server/internal/deptsync/client.go#L177)) performs an **N+1 fan-out**: one request for the tree plus one per department for its users, then an in-memory `userMatches` that only checks `UserID` + `Username`. `SearchDepartments` fetches the whole tree and filters in memory with `departmentMatches`. This is the "pull global data, filter locally" pattern we are removing.

costrict-dept-sync stores the authoritative org projection in `user_department` (`username`, `universal_id`/email, `user_id`/employee id, `position`, `dept_id`) and `department` (`dept_name`, `dept_path`, `dept_id`), but exposes **no keyword search endpoint** and no pagination. The data model supports adding one directly.

## Design Principle

- **Search happens at the source.** costrict-dept-sync owns the org data; it owns the search query. Multica forwards the query, it does not reproduce the dataset.
- **Only added members live in Multica's DB.** Search never reads `multica_member`. The existing add → upsert path is unchanged and already follows this principle.
- **Real-time.** Search results reflect the latest sync. Search results are not cached.

## Architecture

```
members-tab.tsx (unchanged, 200ms debounce)
   │  api.searchDeptUsers / searchDeptDepartments
   ▼
multica /api/dept/{users,departments}/search   (proxy handlers, UNCHANGED — auth boundary)
   │  deptsync.Client.SearchUsers / SearchDepartments   (CHANGED: single live call)
   ▼
costrict-dept-sync GET /v1/users/search?q=&limit=        (NEW)
costrict-dept-sync GET /v1/departments/search?q=&limit=  (NEW)
   │  QueryService.SearchUsers / SearchDepartments
   ▼
UserDeptRepo.SearchByKeyword / DepartmentRepo.SearchByKeyword   (LIKE on indexed columns)
```

The `X-Query-Key` stays server-side in Multica's backend; the browser never sees it. The proxy handlers in [server/internal/handler/dept.go](../../../server/internal/handler/dept.go) are untouched — same `q`/`limit` contract, same `User`/`Department` response shape — so the frontend needs no changes.

## Part 1 — costrict-dept-sync: new real-time search API

Two new endpoints under the existing `/costrict-dept-info/api/v1` group ([pkg/http/router.go:145](costrict-dept-sync/pkg/http/router.go)), which already enforces `X-Query-Key` / `X-Admin-Key` auth.

### Contracts

`GET /v1/users/search?q=<keyword>&limit=<n>`

- Case-insensitive substring match (`status = 1`) across `username`, `universal_id`, `user_id`, `position`.
- Returns `[]DeptUserInfo` — identical field shape to the existing `GET /v1/department/:dept_id/users` (`user_id`, `username`, `universal_id`, `dept_id`, `dept_name`, `is_main`, `position`, `status`, `dept_path`), so Multica's `User` decoder needs no changes.
- **One row per person.** Dedupe by `universal_id` (fall back to `user_id`), keeping the main-department membership (`is_main = 1`). Fill `dept_name` / `dept_path` from the department table, mirroring `queryDeptUsers` ([pkg/service/query.go:463](costrict-dept-sync/pkg/service/query.go)).

`GET /v1/departments/search?q=<keyword>&limit=<n>`

- Case-insensitive substring match (`status = 1`) across `dept_name`, `dept_path`, `dept_id`.
- Returns `[]Department`.

Both: `q` empty → empty list (not an error). `limit` default 20, max 50, clamp invalid values (reuse the validation style in `pkg/http/handler/dept.go`).

### Layered implementation (follow existing patterns)

- **Repository** ([pkg/repository/user_dept.go](costrict-dept-sync/pkg/repository/user_dept.go), `department.go`): add `UserDeptRepo.SearchByKeyword(keyword string, limit int)` and `DepartmentRepo.SearchByKeyword(keyword string, limit int)`. Use `LOWER(col) LIKE LOWER(?)` for **portable** case-insensitive matching across both Postgres and SQLite (Postgres `LIKE` is case-sensitive; SQLite `LIKE` is ASCII-only case-insensitive; `LOWER()` on both sides normalizes them). Chinese names are unaffected by case.
- **Service** ([pkg/service/query.go](costrict-dept-sync/pkg/service/query.go)): add `QueryService.SearchUsers(keyword, limit)` and `QueryService.SearchDepartments(keyword, limit)`. Do **not** cache search results (real-time requirement; the LIKE hits indexed columns and is cheap). Dedupe persons here (order repo results `is_main DESC` so the first row per person is main), and assemble `DeptUserInfo` like `queryDeptUsers` does.
- **Handler** ([pkg/http/handler/dept.go](costrict-dept-sync/pkg/http/handler/dept.go)): add `SearchUsers` and `SearchDepartments` next to `GetDeptUsers`. Read `q` + `limit`, validate, call the service, respond with `resp.Success`.
- **Router** ([pkg/http/router.go](costrict-dept-sync/pkg/http/router.go)): register `apiRouter.GET("/users/search", ...)` and `apiRouter.GET("/departments/search", ...)` inside the existing v1 query group.
- **Frontend client** ([web-ui/src/api/dept.js](costrict-dept-sync/web-ui/src/api/dept.js)): add matching functions for the admin UI (optional but consistent).

### Tests

- Repository tests against an in-memory SQLite fixture: empty `q`, Chinese name substring, email/universal_id match, employee-id match, `limit` clamping, `status = 1` filtering.
- Handler tests via `httptest`: response envelope shape, `limit` validation, empty-`q` returns `[]`.
- Service dedupe test: a person in two departments returns once, with the main department.

## Part 2 — Multica: switch to live query

- `deptsync.Client.SearchUsers` ([server/internal/deptsync/client.go:177](../../../server/internal/deptsync/client.go#L177)): replace the tree-fetch + per-department fan-out + `userMatches` with a single `GET {base}/users/search?q=&limit=` call that decodes the standard envelope into `[]User`. The `User` struct and its `UnmarshalJSON` are unchanged.
- `deptsync.Client.SearchDepartments` ([server/internal/deptsync/client.go:151](../../../server/internal/deptsync/client.go#L151)): replace the tree-fetch + `departmentMatches` with a single `GET {base}/departments/search?q=&limit=` call.
- Remove the helpers that become dead: `userMatches`, `departmentMatches`, and `flattenDepartments` (verify each is unused after the change — `listDepartmentTree`, `findDepartment`, `applyDisplayDeptPaths` are still used by `GetDepartment` / `ListDepartmentUsers` and stay).
- Proxy handlers `SearchDeptUsers` / `SearchDeptDepartments` ([server/internal/handler/dept.go](../../../server/internal/handler/dept.go)): **unchanged**.
- Frontend `members-tab.tsx` and the core API client: **unchanged**.
- Side benefit: `BatchAddDeptMembers` resolves member refs through `SearchUsers`, so it gets the faster path for free.
- Tests: update `deptsync` client tests to spin up an `httptest` server that serves `/users/search` and `/departments/search` and assert the decoded results, error handling, and `limit` clamping.

## Behavior detail: dept_path

Multica currently rebuilds `dept_path` client-side as "ancestor names / current name". The new endpoints return the `dept_path` **stored** by costrict-dept-sync. These formats may differ slightly; `dept_path` is a secondary display field (primary display is `dept_name`). We accept the stored value and do **not** rebuild, trading an exact format match for removing the whole-tree fetch that rebuilding requires.

## What does NOT change

- Multica's `multica_member` table and the add → upsert (`UpsertDeptMember`) path. Only added members persist in Multica.
- The frontend members page and the existing 200ms debounce (already "real-time" enough).
- Casdoor ⇄ dept identity linking via `universal_id`.

## Out of scope

- Pinyin matching for this search box (it has none today; the assignee-picker's client-side pinyin is a separate surface).
- Server-side pagination beyond the simple `limit` clamp (no infinite-scroll UX is needed for this picker).

## Risks

- **Cross-dialect LIKE:** mitigated by `LOWER(col) LIKE LOWER(?)` on both Postgres and SQLite. If query volume ever warrants it, a functional index (`LOWER(username)`) or Postgres `ILIKE` + trigram index can be added later without contract change.
- **`dept_path` format drift:** accepted (see above); only affects a secondary field.
- **N+1 removal correctness:** the new single endpoint must return the same person set the old fan-out did. Covered by porting the dedupe rule (`universal_id` → `user_id`) and by tests on both sides.
