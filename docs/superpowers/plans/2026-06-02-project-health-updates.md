# Project Health & Updates (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Linear-style project updates (markdown body + health status, authored by members or agents) and project start/target dates, with the project's current health derived from its most recent update and shown in the project header and projects list.

**Architecture:** Mirror the existing `project_resource` sub-collection end-to-end (migration → sqlc → handler → router → WS events → core types → api client → query/mutation → realtime → views). Health is NOT a stored project column; it is derived server-side from the latest `project_update` and returned on `ProjectResponse`. The Updates feed is a new tab on the project detail page next to the existing Issues views.

**Tech Stack:** Go (Chi, sqlc, pgx/pgtype), gorilla/websocket event bus; TypeScript, TanStack Query, Zustand, zod, React (shared `packages/views`).

**Reference pattern (read before starting):** `project_resource` — `server/migrations/065_project_resources.up.sql`, `server/pkg/db/queries/project_resource.sql`, `server/internal/handler/project_resource.go`, `packages/core/projects/resource-queries.ts`.

**Conventions that apply throughout:**
- Workspace-scoped everywhere (FK + WHERE-clause guard).
- Path params parsed with `parseUUIDOrBadRequest`; writes use resolved `entity.ID`.
- API responses consumed by UI run through a zod schema via `parseWithFallback` (never bare `as`).
- Mutations optimistic; WS events invalidate queries (never write stores).
- Health values are exactly `on_track | at_risk | off_track`. Update body field is `body` (markdown). Updates have no title and no manual position — ordered by `created_at DESC`.
- After each backend task touching SQL, run `make sqlc`. Run `cd server && gofmt -w .` before committing Go.

---

## File Structure

**Backend (create):**
- `server/migrations/112_project_dates.up.sql` / `.down.sql` — add `start_date`/`target_date` to `project`.
- `server/migrations/113_project_updates.up.sql` / `.down.sql` — `project_update` table.
- `server/pkg/db/queries/project_update.sql` — sqlc queries.
- `server/internal/handler/project_update.go` — CRUD handlers.
- `server/internal/handler/project_update_test.go` — handler tests.

**Backend (modify):**
- `server/pkg/protocol/events.go` — add `project_update:*` event constants.
- `server/cmd/server/router.go` — nest `/updates` routes.
- `server/internal/handler/project.go` — extend `ProjectResponse` with `health`, `last_update_at`, `start_date`, `target_date`; batch-load latest update in `ListProjects`/`GetProject`; accept dates in create/update.
- `server/pkg/db/queries/project.sql` — add date columns to create/update; add `GetLatestUpdatesForProjects`.

**Frontend (create):**
- `packages/core/projects/update-queries.ts` — query keys/options + mutations for updates.
- `packages/views/projects/components/health-pill.tsx` — health indicator.
- `packages/views/projects/components/project-update-card.tsx` — one update.
- `packages/views/projects/components/project-update-composer.tsx` — compose new update.
- `packages/views/projects/components/project-updates-tab.tsx` — feed + composer.

**Frontend (modify):**
- `packages/core/types/project.ts` — `ProjectHealth`, `ProjectUpdate`, request types, extend `Project`/requests.
- `packages/core/api/client.ts` — update methods.
- `packages/core/api/projects-schema.ts` (create) — zod schemas + parseWithFallback wiring used by client.
- `packages/core/realtime/use-realtime-sync.ts` — `project_update` invalidation.
- `packages/views/projects/components/project-detail.tsx` — Issues/Updates tab switcher; header dates + health pill.

**Tests (create):**
- `packages/core/api/projects-schema.test.ts` — malformed-response handling.
- `packages/core/projects/update-queries.test.ts` — key + mutation invalidation.
- `packages/views/projects/components/health-pill.test.tsx` — enum-drift fallback.
- `packages/views/projects/components/project-updates-tab.test.tsx` — feed/empty/compose.

---

## Task 1: Migration — project dates

**Files:**
- Create: `server/migrations/112_project_dates.up.sql`
- Create: `server/migrations/112_project_dates.down.sql`

- [ ] **Step 1: Write the up migration**

`server/migrations/112_project_dates.up.sql`:
```sql
-- Project scheduling dates. Nullable: a project may have neither, either, or both.
-- start_date/target_date give the health signal a timeline to reference.
ALTER TABLE project ADD COLUMN start_date DATE;
ALTER TABLE project ADD COLUMN target_date DATE;
```

- [ ] **Step 2: Write the down migration**

`server/migrations/112_project_dates.down.sql`:
```sql
ALTER TABLE project DROP COLUMN IF EXISTS target_date;
ALTER TABLE project DROP COLUMN IF EXISTS start_date;
```

- [ ] **Step 3: Apply and verify**

Run: `make migrate-up`
Expected: migration 112 applies with no error. Verify with `cd server && go run ./cmd/... ` is not needed — just confirm `make migrate-up` exits 0.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/112_project_dates.up.sql server/migrations/112_project_dates.down.sql
git commit -m "feat(projects): add start_date/target_date columns"
```

---

## Task 2: Migration — project_update table

**Files:**
- Create: `server/migrations/113_project_updates.up.sql`
- Create: `server/migrations/113_project_updates.down.sql`

- [ ] **Step 1: Write the up migration**

`server/migrations/113_project_updates.up.sql`:
```sql
-- Project Updates: narrative health posts on a project, authored by a member or
-- an agent. The project's current health is derived from the most recent update
-- (see GetLatestUpdatesForProjects). Ordered by created_at; no manual position.
CREATE TABLE project_update (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    health       TEXT NOT NULL CHECK (health IN ('on_track', 'at_risk', 'off_track')),
    body         TEXT NOT NULL DEFAULT '',
    author_type  TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_update_project ON project_update(project_id, created_at DESC);
CREATE INDEX idx_project_update_workspace ON project_update(workspace_id);
```

- [ ] **Step 2: Write the down migration**

`server/migrations/113_project_updates.down.sql`:
```sql
DROP TABLE IF EXISTS project_update;
```

- [ ] **Step 3: Apply and verify**

Run: `make migrate-up`
Expected: migration 113 applies, exit 0.

- [ ] **Step 4: Commit**

```bash
git add server/migrations/113_project_updates.up.sql server/migrations/113_project_updates.down.sql
git commit -m "feat(projects): add project_update table"
```

---

## Task 3: sqlc queries for project_update

**Files:**
- Create: `server/pkg/db/queries/project_update.sql`
- Modify: `server/pkg/db/queries/project.sql` (add `GetLatestUpdatesForProjects`)

- [ ] **Step 1: Write project_update.sql**

`server/pkg/db/queries/project_update.sql`:
```sql
-- name: ListProjectUpdates :many
SELECT * FROM project_update
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: GetProjectUpdateInWorkspace :one
SELECT * FROM project_update
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProjectUpdate :one
INSERT INTO project_update (
    project_id, workspace_id, health, body, author_type, author_id
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateProjectUpdate :one
UPDATE project_update
SET health     = COALESCE(sqlc.narg('health'), health),
    body       = COALESCE(sqlc.narg('body'), body),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProjectUpdate :exec
DELETE FROM project_update WHERE id = $1;
```

- [ ] **Step 2: Add the batch latest-update query to project.sql**

Append to `server/pkg/db/queries/project.sql`:
```sql
-- name: GetLatestUpdatesForProjects :many
-- One row per project that has at least one update: the most recent update's
-- health and created_at. Used to derive each project's current health in list/detail.
SELECT DISTINCT ON (project_id)
    project_id,
    health,
    created_at AS last_update_at
FROM project_update
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
ORDER BY project_id, created_at DESC;
```

- [ ] **Step 3: Regenerate sqlc**

Run: `make sqlc`
Expected: exit 0; new methods `ListProjectUpdates`, `GetProjectUpdateInWorkspace`, `CreateProjectUpdate`, `UpdateProjectUpdate`, `DeleteProjectUpdate`, `GetLatestUpdatesForProjects` appear in `server/pkg/db/generated/`.

- [ ] **Step 4: Verify it builds**

Run: `cd server && go build ./...`
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/db/queries/project_update.sql server/pkg/db/queries/project.sql server/pkg/db/generated/
git commit -m "feat(projects): sqlc queries for project_update + latest-update batch"
```

---

## Task 4: WS event constants

**Files:**
- Modify: `server/pkg/protocol/events.go`

- [ ] **Step 1: Add event constants**

In `server/pkg/protocol/events.go`, in the project events block (after `EventProjectResourceDeleted`), add:
```go
	EventProjectUpdateCreated = "project_update:created"
	EventProjectUpdateUpdated = "project_update:updated"
	EventProjectUpdateDeleted = "project_update:deleted"
```

- [ ] **Step 2: Verify build**

Run: `cd server && go build ./...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add server/pkg/protocol/events.go
git commit -m "feat(projects): project_update WS event constants"
```

---

## Task 5: project_update handler

**Files:**
- Create: `server/internal/handler/project_update.go`

Notes: reuse `loadProjectForResource` (already general — resolves project + workspace ownership). Author defaults to the authenticated member; an agent/daemon caller may supply `author_type`/`author_id` to post as an agent.

- [ ] **Step 1: Write the handler file**

`server/internal/handler/project_update.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "multica/server/pkg/db/generated"
	"multica/server/pkg/protocol"
)

// ProjectUpdateResponse is the JSON shape returned by the project update API.
type ProjectUpdateResponse struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
	Health      string `json:"health"`
	Body        string `json:"body"`
	AuthorType  string `json:"author_type"`
	AuthorID    string `json:"author_id"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func projectUpdateToResponse(u db.ProjectUpdate) ProjectUpdateResponse {
	return ProjectUpdateResponse{
		ID:          uuidToString(u.ID),
		ProjectID:   uuidToString(u.ProjectID),
		WorkspaceID: uuidToString(u.WorkspaceID),
		Health:      u.Health,
		Body:        u.Body,
		AuthorType:  u.AuthorType,
		AuthorID:    uuidToString(u.AuthorID),
		CreatedAt:   timestampToString(u.CreatedAt),
		UpdatedAt:   timestampToString(u.UpdatedAt),
	}
}

func validProjectHealth(h string) bool {
	switch h {
	case "on_track", "at_risk", "off_track":
		return true
	default:
		return false
	}
}

// CreateProjectUpdateRequest is the body for POST /api/projects/{id}/updates.
type CreateProjectUpdateRequest struct {
	Health string `json:"health"`
	Body   string `json:"body"`
	// Optional agent authorship. When omitted, the update is authored by the
	// authenticated member. Agent/daemon callers set these to post as an agent.
	AuthorType *string `json:"author_type"`
	AuthorID   *string `json:"author_id"`
}

// UpdateProjectUpdateRequest is the body for PUT /api/projects/{id}/updates/{updateId}.
// Every field optional; omitted fields keep their current value.
type UpdateProjectUpdateRequest struct {
	Health *string `json:"health"`
	Body   *string `json:"body"`
}

// ListProjectUpdates returns a project's updates, newest first.
func (h *Handler) ListProjectUpdates(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	updates, err := h.Queries.ListProjectUpdates(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project updates")
		return
	}
	resp := make([]ProjectUpdateResponse, len(updates))
	for i, u := range updates {
		resp[i] = projectUpdateToResponse(u)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updates": resp, "total": len(resp)})
}

// CreateProjectUpdate posts a new update to a project.
func (h *Handler) CreateProjectUpdate(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreateProjectUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Health = strings.TrimSpace(req.Health)
	if !validProjectHealth(req.Health) {
		writeError(w, http.StatusBadRequest, "health must be one of on_track, at_risk, off_track")
		return
	}

	// Resolve author. Default: authenticated member. Agent callers may override.
	authorType := "member"
	authorIDStr := userID
	if req.AuthorType != nil {
		switch *req.AuthorType {
		case "member", "agent":
			authorType = *req.AuthorType
		default:
			writeError(w, http.StatusBadRequest, "author_type must be member or agent")
			return
		}
	}
	if req.AuthorID != nil && strings.TrimSpace(*req.AuthorID) != "" {
		authorIDStr = strings.TrimSpace(*req.AuthorID)
	}
	authorUUID, ok := parseUUIDOrBadRequest(w, authorIDStr, "author id")
	if !ok {
		return
	}

	update, err := h.Queries.CreateProjectUpdate(r.Context(), db.CreateProjectUpdateParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
		Health:      req.Health,
		Body:        req.Body,
		AuthorType:  authorType,
		AuthorID:    authorUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project update")
		return
	}

	resp := projectUpdateToResponse(update)
	h.publish(
		protocol.EventProjectUpdateCreated,
		uuidToString(project.WorkspaceID),
		authorType,
		authorIDStr,
		map[string]any{"update": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusCreated, resp)
}

// UpdateProjectUpdate edits an existing update's health/body.
func (h *Handler) UpdateProjectUpdate(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	updateUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "updateId"), "update id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	existing, err := h.Queries.GetProjectUpdateInWorkspace(r.Context(), db.GetProjectUpdateInWorkspaceParams{
		ID: updateUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}
	if uuidToString(existing.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}

	var req UpdateProjectUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var health pgtype.Text
	if req.Health != nil {
		h2 := strings.TrimSpace(*req.Health)
		if !validProjectHealth(h2) {
			writeError(w, http.StatusBadRequest, "health must be one of on_track, at_risk, off_track")
			return
		}
		health = pgtype.Text{String: h2, Valid: true}
	}
	var body pgtype.Text
	if req.Body != nil {
		body = pgtype.Text{String: *req.Body, Valid: true}
	}

	updated, err := h.Queries.UpdateProjectUpdate(r.Context(), db.UpdateProjectUpdateParams{
		ID:     existing.ID,
		Health: health,
		Body:   body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project update")
		return
	}

	resp := projectUpdateToResponse(updated)
	h.publish(
		protocol.EventProjectUpdateUpdated,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"update": resp, "project_id": uuidToString(project.ID)},
	)
	writeJSON(w, http.StatusOK, resp)
}

// DeleteProjectUpdate removes an update from a project.
func (h *Handler) DeleteProjectUpdate(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	updateUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "updateId"), "update id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	existing, err := h.Queries.GetProjectUpdateInWorkspace(r.Context(), db.GetProjectUpdateInWorkspaceParams{
		ID: updateUUID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}
	if uuidToString(existing.ProjectID) != uuidToString(project.ID) {
		writeError(w, http.StatusNotFound, "project update not found")
		return
	}
	if err := h.Queries.DeleteProjectUpdate(r.Context(), existing.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project update")
		return
	}
	h.publish(
		protocol.EventProjectUpdateDeleted,
		uuidToString(project.WorkspaceID),
		"member",
		userID,
		map[string]any{"project_id": uuidToString(project.ID), "update_id": uuidToString(existing.ID)},
	)
	w.WriteHeader(http.StatusNoContent)
}
```

> **Note for implementer:** verify the import path of the generated db package and the `protocol` package against an existing handler (e.g. the top of `server/internal/handler/project_resource.go`) and match it exactly — the module path may differ from `multica/server/...`. Also confirm the `UpdateProjectUpdateParams` field types from the generated code (`pgtype.Text` for the `COALESCE(sqlc.narg(...))` columns).

- [ ] **Step 2: Verify build**

Run: `cd server && gofmt -w . && go build ./...`
Expected: exit 0. If the db import path differs, fix the import to match `project_resource.go`.

- [ ] **Step 3: Commit**

```bash
git add server/internal/handler/project_update.go
git commit -m "feat(projects): project_update CRUD handlers"
```

---

## Task 6: Router wiring

**Files:**
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Add nested routes**

In `server/cmd/server/router.go`, inside the `r.Route("/{id}", ...)` block under `/api/projects` (right after the resource routes), add:
```go
		r.Get("/updates", h.ListProjectUpdates)
		r.Post("/updates", h.CreateProjectUpdate)
		r.Put("/updates/{updateId}", h.UpdateProjectUpdate)
		r.Delete("/updates/{updateId}", h.DeleteProjectUpdate)
```

- [ ] **Step 2: Verify build**

Run: `cd server && go build ./...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(projects): wire project update routes"
```

---

## Task 7: Derive health + dates on ProjectResponse

**Files:**
- Modify: `server/internal/handler/project.go`
- Modify: `server/pkg/db/queries/project.sql` (create/update params for dates)

This adds `health`, `last_update_at`, `start_date`, `target_date` to the project API, batch-loads the latest update in `ListProjects`, single-loads it in `GetProject`, and accepts the dates in create/update.

- [ ] **Step 1: Add date columns to the project create/update queries**

In `server/pkg/db/queries/project.sql`, update `CreateProject` and `UpdateProject` to include the date columns. For `CreateProject`, add `start_date, target_date` to the INSERT column list and as new positional params. For `UpdateProject`, add:
```sql
    start_date  = COALESCE(sqlc.narg('start_date'), start_date),
    target_date = COALESCE(sqlc.narg('target_date'), target_date),
```
Then run `make sqlc`.

Run: `make sqlc && cd server && go build ./...`
Expected: regenerated params include `StartDate pgtype.Date` / `TargetDate pgtype.Date`. Build may now FAIL in `project.go` because the create/update call sites don't pass the new params — that's expected; fixed in Step 3.

- [ ] **Step 2: Extend ProjectResponse**

In `server/internal/handler/project.go`, add to the `ProjectResponse` struct (after the existing fields):
```go
	StartDate    *string `json:"start_date"`
	TargetDate   *string `json:"target_date"`
	Health       *string `json:"health"`
	LastUpdateAt *string `json:"last_update_at"`
```
And in the function that maps a `db.Project` to `ProjectResponse`, set:
```go
	resp.StartDate = dateToPtr(project.StartDate)
	resp.TargetDate = dateToPtr(project.TargetDate)
```
If a `dateToPtr(pgtype.Date) *string` helper does not already exist in `handler.go`, add it next to `timestampToString`:
```go
func dateToPtr(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}
```

- [ ] **Step 3: Pass dates through create/update handlers**

In `CreateProject` and `UpdateProject` handlers in `project.go`, decode `start_date`/`target_date` from the request body (string `YYYY-MM-DD`, nullable) and pass them as `pgtype.Date` to the query params. Add a helper if none exists:
```go
func parseDateParam(s *string) pgtype.Date {
	if s == nil || strings.TrimSpace(*s) == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*s))
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
```
Add the request-struct fields `StartDate *string `json:"start_date"`` and `TargetDate *string `json:"target_date"`` to the create/update request structs, and wire `StartDate: parseDateParam(req.StartDate)` etc. into the params.

- [ ] **Step 4: Batch-load latest update health in ListProjects**

In `ListProjects`, after collecting the project IDs (where the existing code batch-loads issue stats / resource counts), add:
```go
	latest, err := h.Queries.GetLatestUpdatesForProjects(r.Context(), projectIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project health")
		return
	}
	healthByProject := make(map[string]db.GetLatestUpdatesForProjectsRow, len(latest))
	for _, row := range latest {
		healthByProject[uuidToString(row.ProjectID)] = row
	}
```
Then when building each `ProjectResponse`, set:
```go
	if row, found := healthByProject[uuidToString(project.ID)]; found {
		hv := row.Health
		resp.Health = &hv
		ts := timestampToString(row.LastUpdateAt)
		resp.LastUpdateAt = &ts
	}
```
> **Note:** confirm the generated row type name (`GetLatestUpdatesForProjectsRow`) and that `project_ids` maps to a `[]pgtype.UUID` arg matching how `GetProjectResourceCounts` is called in this file — mirror that call exactly.

- [ ] **Step 5: Single-load latest update in GetProject**

In `GetProject`, after loading the project, derive health from the same batch query with a single-element slice:
```go
	latest, _ := h.Queries.GetLatestUpdatesForProjects(r.Context(), []pgtype.UUID{project.ID})
	if len(latest) > 0 {
		hv := latest[0].Health
		resp.Health = &hv
		ts := timestampToString(latest[0].LastUpdateAt)
		resp.LastUpdateAt = &ts
	}
```

- [ ] **Step 6: Verify build**

Run: `cd server && gofmt -w . && go build ./...`
Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/project.go server/pkg/db/queries/project.sql server/pkg/db/generated/
git commit -m "feat(projects): derive health + expose dates on ProjectResponse"
```

---

## Task 8: Go handler tests

**Files:**
- Create: `server/internal/handler/project_update_test.go`

Mirror the setup used in an existing project test (find `server/internal/handler/project*_test.go` or `project_resource_test.go` and reuse its fixture/bootstrap helpers — workspace, user, project creation, and the test `Handler` constructor). Use those exact helpers; do not invent new ones.

- [ ] **Step 1: Write the failing tests**

`server/internal/handler/project_update_test.go` (adapt helper names to the existing test file's helpers):
```go
package handler

import (
	"net/http"
	"testing"
)

// TestCreateAndListProjectUpdate posts an update and reads it back; verifies the
// project's derived health reflects the latest update.
func TestCreateAndListProjectUpdate(t *testing.T) {
	env := newTestEnv(t)          // <-- use the existing bootstrap helper
	defer env.cleanup()
	ws := env.createWorkspace(t)
	proj := env.createProject(t, ws)

	body := `{"health":"at_risk","body":"Slipping on the API work."}`
	rec := env.do(t, http.MethodPost, "/api/projects/"+proj.ID+"/updates", ws.ID, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	listRec := env.do(t, http.MethodGet, "/api/projects/"+proj.ID+"/updates", ws.ID, "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", listRec.Code)
	}
	if !contains(listRec.Body.String(), "Slipping on the API work.") {
		t.Fatalf("list missing body: %s", listRec.Body.String())
	}

	projRec := env.do(t, http.MethodGet, "/api/projects/"+proj.ID, ws.ID, "")
	if !contains(projRec.Body.String(), `"health":"at_risk"`) {
		t.Fatalf("project health not derived: %s", projRec.Body.String())
	}
}

// TestCreateProjectUpdateRejectsBadHealth verifies enum validation.
func TestCreateProjectUpdateRejectsBadHealth(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	ws := env.createWorkspace(t)
	proj := env.createProject(t, ws)

	rec := env.do(t, http.MethodPost, "/api/projects/"+proj.ID+"/updates", ws.ID, `{"health":"green","body":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad health, got %d", rec.Code)
	}
}

// TestProjectUpdateWorkspaceIsolation verifies an update cannot be read from another workspace.
func TestProjectUpdateWorkspaceIsolation(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()
	wsA := env.createWorkspace(t)
	wsB := env.createWorkspace(t)
	proj := env.createProject(t, wsA)

	env.do(t, http.MethodPost, "/api/projects/"+proj.ID+"/updates", wsA.ID, `{"health":"on_track","body":"ok"}`)
	rec := env.do(t, http.MethodGet, "/api/projects/"+proj.ID+"/updates", wsB.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace list: want 404, got %d", rec.Code)
	}
}
```
> **Note for implementer:** replace `newTestEnv`, `env.createWorkspace`, `env.createProject`, `env.do`, `env.cleanup`, and `contains` with the actual helpers in the existing handler test files. If the existing tests use a different shape (e.g. table-driven with a shared `setupTestHandler`), follow that shape instead — the assertions above are what matters.

- [ ] **Step 2: Run to verify they fail/compile-fail**

Run: `cd server && go test ./internal/handler/ -run TestCreateAndListProjectUpdate -v`
Expected: FAIL (until helpers are correctly wired). Fix helper wiring until the test runs and passes against the real handlers.

- [ ] **Step 3: Make tests pass**

Adjust helper usage until all three tests pass. Implementation already exists (Tasks 5–7), so passing means the wiring is correct.

Run: `cd server && go test ./internal/handler/ -run TestProjectUpdate -v && go test ./internal/handler/ -run TestCreateAndListProjectUpdate -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/handler/project_update_test.go
git commit -m "test(projects): project_update handler tests"
```

---

## Task 9: Core types

**Files:**
- Modify: `packages/core/types/project.ts`

- [ ] **Step 1: Add update + health types and extend Project**

In `packages/core/types/project.ts`:
- Add:
```typescript
export type ProjectHealth = "on_track" | "at_risk" | "off_track";

export interface ProjectUpdate {
  id: string;
  project_id: string;
  workspace_id: string;
  health: ProjectHealth;
  body: string;
  author_type: "member" | "agent";
  author_id: string;
  created_at: string;
  updated_at: string;
}

export interface ListProjectUpdatesResponse {
  updates: ProjectUpdate[];
  total: number;
}

export interface CreateProjectUpdateRequest {
  health: ProjectHealth;
  body: string;
  author_type?: "member" | "agent";
  author_id?: string;
}

export interface UpdateProjectUpdateRequest {
  health?: ProjectHealth;
  body?: string;
}
```
- Extend `Project` with:
```typescript
  start_date: string | null;
  target_date: string | null;
  health: ProjectHealth | null;
  last_update_at: string | null;
```
- Extend `CreateProjectRequest` with `start_date?: string; target_date?: string;`
- Extend `UpdateProjectRequest` with `start_date?: string | null; target_date?: string | null;`

- [ ] **Step 2: Verify typecheck of the package**

Run: `pnpm --filter @multica/core typecheck` (or `pnpm typecheck` if no package-level script)
Expected: may FAIL where new non-optional `Project` fields are constructed in tests/mocks — note these and fix mocks if any are in core. If failures are only in views/web, they'll be handled in later tasks.

- [ ] **Step 3: Commit**

```bash
git add packages/core/types/project.ts
git commit -m "feat(projects): types for project updates, health, dates"
```

---

## Task 10: zod schema + parseWithFallback wiring

**Files:**
- Create: `packages/core/api/projects-schema.ts`
- Modify: `packages/core/api/client.ts`

- [ ] **Step 1: Write the schema module**

`packages/core/api/projects-schema.ts`:
```typescript
import { z } from "zod";
import type { ListProjectUpdatesResponse, ProjectUpdate } from "../types/project";

const ProjectHealthSchema = z.enum(["on_track", "at_risk", "off_track"]);

export const ProjectUpdateSchema = z.object({
  id: z.string(),
  project_id: z.string(),
  workspace_id: z.string(),
  health: ProjectHealthSchema,
  body: z.string().default(""),
  author_type: z.enum(["member", "agent"]),
  author_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const ListProjectUpdatesResponseSchema = z.object({
  updates: z.array(ProjectUpdateSchema).default([]),
  total: z.number().default(0),
});

export const EMPTY_PROJECT_UPDATES: ListProjectUpdatesResponse = {
  updates: [],
  total: 0,
};

export type ParsedProjectUpdate = ProjectUpdate;
```

- [ ] **Step 2: Use the schema in the client list method**

In `packages/core/api/client.ts`, add the update methods. The list method parses through the schema (matching the existing `parseWithFallback` usage in this file):
```typescript
async listProjectUpdates(projectId: string): Promise<ListProjectUpdatesResponse> {
  const raw = await this.fetch<unknown>(`/api/projects/${projectId}/updates`);
  return parseWithFallback(raw, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, {
    endpoint: "GET /api/projects/{id}/updates",
  });
}

async createProjectUpdate(projectId: string, data: CreateProjectUpdateRequest): Promise<ProjectUpdate> {
  return this.fetch(`/api/projects/${projectId}/updates`, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

async updateProjectUpdate(projectId: string, updateId: string, data: UpdateProjectUpdateRequest): Promise<ProjectUpdate> {
  return this.fetch(`/api/projects/${projectId}/updates/${updateId}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

async deleteProjectUpdate(projectId: string, updateId: string): Promise<void> {
  await this.fetch(`/api/projects/${projectId}/updates/${updateId}`, { method: "DELETE" });
}
```
Add the imports at the top of `client.ts`:
```typescript
import { ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES } from "./projects-schema";
import type { ProjectUpdate, ListProjectUpdatesResponse, CreateProjectUpdateRequest, UpdateProjectUpdateRequest } from "../types/project";
```
> **Note:** match the existing import style and `parseWithFallback` import path already present in `client.ts`.

- [ ] **Step 3: Verify typecheck**

Run: `pnpm --filter @multica/core typecheck`
Expected: exit 0 (or only pre-existing unrelated errors).

- [ ] **Step 4: Commit**

```bash
git add packages/core/api/projects-schema.ts packages/core/api/client.ts
git commit -m "feat(projects): client methods + zod schema for project updates"
```

---

## Task 11: Schema malformed-response test

**Files:**
- Create: `packages/core/api/projects-schema.test.ts`

- [ ] **Step 1: Write the test**

`packages/core/api/projects-schema.test.ts`:
```typescript
import { describe, it, expect } from "vitest";
import { parseWithFallback } from "./schema";
import { ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES } from "./projects-schema";

describe("ListProjectUpdatesResponseSchema", () => {
  it("returns fallback when updates array is missing", () => {
    const result = parseWithFallback({}, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, {
      endpoint: "test",
    });
    expect(result).toEqual(EMPTY_PROJECT_UPDATES);
  });

  it("returns fallback when an update has an unknown health value", () => {
    const bad = { updates: [{ id: "1", project_id: "p", workspace_id: "w", health: "green", body: "x", author_type: "member", author_id: "a", created_at: "t", updated_at: "t" }], total: 1 };
    const result = parseWithFallback(bad, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, {
      endpoint: "test",
    });
    expect(result).toEqual(EMPTY_PROJECT_UPDATES);
  });

  it("passes a well-formed response through", () => {
    const good = { updates: [{ id: "1", project_id: "p", workspace_id: "w", health: "on_track", body: "ok", author_type: "member", author_id: "a", created_at: "t", updated_at: "t" }], total: 1 };
    const result = parseWithFallback(good, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, {
      endpoint: "test",
    });
    expect(result.updates).toHaveLength(1);
    expect(result.updates[0].health).toBe("on_track");
  });
});
```
> **Note:** confirm `parseWithFallback` is exported from `./schema` (per the reference); adjust the import path if it lives elsewhere.

- [ ] **Step 2: Run the test**

Run: `pnpm --filter @multica/core exec vitest run api/projects-schema.test.ts`
Expected: 3 passing.

- [ ] **Step 3: Commit**

```bash
git add packages/core/api/projects-schema.test.ts
git commit -m "test(projects): malformed project-update response handling"
```

---

## Task 12: Query keys/options + mutations for updates

**Files:**
- Create: `packages/core/projects/update-queries.ts`

- [ ] **Step 1: Write the query + mutation module**

`packages/core/projects/update-queries.ts`:
```typescript
import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import type {
  ListProjectUpdatesResponse,
  ProjectUpdate,
  CreateProjectUpdateRequest,
  UpdateProjectUpdateRequest,
} from "../types/project";

export const projectUpdateKeys = {
  list: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "updates"] as const,
};

export function projectUpdatesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectUpdateKeys.list(wsId, projectId),
    queryFn: () => api.listProjectUpdates(projectId),
    select: (data: ListProjectUpdatesResponse) => data.updates,
  });
}

export function useCreateProjectUpdate(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateProjectUpdateRequest) => api.createProjectUpdate(projectId, data),
    onSuccess: (created: ProjectUpdate) => {
      qc.setQueryData<ListProjectUpdatesResponse>(
        projectUpdateKeys.list(wsId, projectId),
        (old) =>
          old && !old.updates.some((u) => u.id === created.id)
            ? { ...old, updates: [created, ...old.updates], total: old.total + 1 }
            : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectUpdateKeys.list(wsId, projectId) });
      // Refresh derived health on the project list + detail.
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
    },
  });
}

export function useUpdateProjectUpdate(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ updateId, ...data }: { updateId: string } & UpdateProjectUpdateRequest) =>
      api.updateProjectUpdate(projectId, updateId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectUpdateKeys.list(wsId, projectId) });
      qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
    },
  });
}

export function useDeleteProjectUpdate(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (updateId: string) => api.deleteProjectUpdate(projectId, updateId),
    onMutate: (updateId: string) => {
      const key = projectUpdateKeys.list(wsId, projectId);
      const prev = qc.getQueryData<ListProjectUpdatesResponse>(key);
      qc.setQueryData<ListProjectUpdatesResponse>(key, (old) =>
        old ? { ...old, updates: old.updates.filter((u) => u.id !== updateId), total: Math.max(0, old.total - 1) } : old,
      );
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(projectUpdateKeys.list(wsId, projectId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectUpdateKeys.list(wsId, projectId) });
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
    },
  });
}
```
> **Note:** confirm `projectKeys` is exported from `./queries` with `list`/`detail` signatures matching the reference; if `api` is imported differently in sibling files, match that.

- [ ] **Step 2: Verify typecheck**

Run: `pnpm --filter @multica/core typecheck`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add packages/core/projects/update-queries.ts
git commit -m "feat(projects): query options + mutations for project updates"
```

---

## Task 13: Mutation invalidation test

**Files:**
- Create: `packages/core/projects/update-queries.test.ts`

- [ ] **Step 1: Write the test**

`packages/core/projects/update-queries.test.ts`:
```typescript
import { describe, it, expect } from "vitest";
import { projectUpdateKeys } from "./update-queries";
import { projectKeys } from "./queries";

describe("projectUpdateKeys", () => {
  it("nests updates under the project detail key", () => {
    const wsId = "ws1";
    const projectId = "p1";
    expect(projectUpdateKeys.list(wsId, projectId)).toEqual([
      ...projectKeys.detail(wsId, projectId),
      "updates",
    ]);
  });

  it("changes key when workspace changes (workspace isolation)", () => {
    expect(projectUpdateKeys.list("wsA", "p1")).not.toEqual(
      projectUpdateKeys.list("wsB", "p1"),
    );
  });
});
```

- [ ] **Step 2: Run the test**

Run: `pnpm --filter @multica/core exec vitest run projects/update-queries.test.ts`
Expected: 2 passing.

- [ ] **Step 3: Commit**

```bash
git add packages/core/projects/update-queries.test.ts
git commit -m "test(projects): project-update query key isolation"
```

---

## Task 14: Realtime invalidation

**Files:**
- Modify: `packages/core/realtime/use-realtime-sync.ts`

- [ ] **Step 1: Add the project_update entry**

In the `refreshMap` (or equivalent prefix→handler map) in `packages/core/realtime/use-realtime-sync.ts`, add an entry keyed `project_update` next to the existing `project` entry:
```typescript
  project_update: () => {
    const wsId = getCurrentWsId();
    if (!wsId) return;
    // Invalidate every project's updates list + the project list/detail so
    // derived health refreshes.
    qc.invalidateQueries({
      predicate: (query) => {
        const key = query.queryKey as unknown[];
        return key[0] === "projects" && key[1] === wsId;
      },
    });
  },
```
> **Note:** the WS event type is `project_update:created` etc.; confirm the map is keyed by the prefix before `:` (matching how `project_resource` or `project` is keyed). If the map keys on the full event string, add all three (`project_update:created|updated|deleted`).

- [ ] **Step 2: Verify typecheck**

Run: `pnpm --filter @multica/core typecheck`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add packages/core/realtime/use-realtime-sync.ts
git commit -m "feat(projects): realtime invalidation for project updates"
```

---

## Task 15: HealthPill component

**Files:**
- Create: `packages/views/projects/components/health-pill.tsx`
- Test: `packages/views/projects/components/health-pill.test.tsx`

- [ ] **Step 1: Write the failing test**

`packages/views/projects/components/health-pill.test.tsx`:
```typescript
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { HealthPill } from "./health-pill";

describe("HealthPill", () => {
  it("renders a label for a known health value", () => {
    render(<HealthPill health="at_risk" />);
    expect(screen.getByText(/at risk/i)).toBeInTheDocument();
  });

  it("renders a neutral fallback for an unknown value (enum drift)", () => {
    // @ts-expect-error testing drift
    render(<HealthPill health="exploded" />);
    expect(screen.getByText(/no update|unknown/i)).toBeInTheDocument();
  });

  it("renders 'No update' when health is null", () => {
    render(<HealthPill health={null} />);
    expect(screen.getByText(/no update/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm --filter @multica/views exec vitest run projects/components/health-pill.test.tsx`
Expected: FAIL (module not found).

- [ ] **Step 3: Write the component**

`packages/views/projects/components/health-pill.tsx`:
```typescript
import type { ProjectHealth } from "@multica/core/types/project";
import { cn } from "@multica/ui/lib/utils";

interface HealthPillProps {
  health: ProjectHealth | null | undefined;
  className?: string;
}

const CONFIG: Record<ProjectHealth, { label: string; dot: string; text: string }> = {
  on_track: { label: "On track", dot: "bg-emerald-500", text: "text-emerald-600 dark:text-emerald-400" },
  at_risk: { label: "At risk", dot: "bg-amber-500", text: "text-amber-600 dark:text-amber-400" },
  off_track: { label: "Off track", dot: "bg-red-500", text: "text-red-600 dark:text-red-400" },
};

export function HealthPill({ health, className }: HealthPillProps) {
  const cfg = health ? CONFIG[health as ProjectHealth] : undefined;
  if (!cfg) {
    return (
      <span className={cn("inline-flex items-center gap-1.5 text-xs text-muted-foreground", className)}>
        <span className="h-2 w-2 rounded-full bg-muted-foreground/40" />
        No update
      </span>
    );
  }
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", cfg.text, className)}>
      <span className={cn("h-2 w-2 rounded-full", cfg.dot)} />
      {cfg.label}
    </span>
  );
}
```
> **Note:** confirm the `cn` import path used elsewhere in `packages/views` (e.g. grep an existing component). The project's CSS rules discourage hardcoded colors generally, but status indicators are an established exception — match how `project-badge.tsx` colors statuses if it uses a different convention. If i18n is required for labels in this codebase, wire `useT` like other view components instead of the literal strings.

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm --filter @multica/views exec vitest run projects/components/health-pill.test.tsx`
Expected: 3 passing.

- [ ] **Step 5: Commit**

```bash
git add packages/views/projects/components/health-pill.tsx packages/views/projects/components/health-pill.test.tsx
git commit -m "feat(projects): HealthPill component"
```

---

## Task 16: ProjectUpdateCard component

**Files:**
- Create: `packages/views/projects/components/project-update-card.tsx`

- [ ] **Step 1: Write the component**

`packages/views/projects/components/project-update-card.tsx`:
```typescript
import type { ProjectUpdate } from "@multica/core/types/project";
import { ActorAvatar } from "../../common/actor-avatar";
import { useTimeAgo } from "../../i18n/use-time-ago";
import { HealthPill } from "./health-pill";

interface ProjectUpdateCardProps {
  update: ProjectUpdate;
  canModerate?: boolean;
  onDelete?: (updateId: string) => void;
}

export function ProjectUpdateCard({ update, canModerate, onDelete }: ProjectUpdateCardProps) {
  const timeAgo = useTimeAgo();
  return (
    <article className="rounded-lg border border-border bg-card p-4">
      <header className="flex items-center gap-2">
        <ActorAvatar actorType={update.author_type} actorId={update.author_id} size={20} enableHoverCard />
        <span className="text-xs text-muted-foreground">{timeAgo(update.created_at)}</span>
        <span className="ml-auto">
          <HealthPill health={update.health} />
        </span>
        {canModerate && onDelete && (
          <button
            type="button"
            onClick={() => onDelete(update.id)}
            className="text-xs text-muted-foreground hover:text-destructive"
            aria-label="Delete update"
          >
            Delete
          </button>
        )}
      </header>
      <div className="mt-3 whitespace-pre-wrap text-sm text-foreground">{update.body}</div>
    </article>
  );
}
```
> **Note:** `ActorAvatar` lives at `packages/views/common/actor-avatar.tsx` and `useTimeAgo` at `packages/views/i18n/use-time-ago.ts` per the reference — confirm relative import depth from `projects/components/`. For rendering markdown rather than plain text, reuse the read-only renderer the project description/comments use (find how `project.description` is displayed); if updates should render markdown, swap the `<div>` for that renderer. Plain text is acceptable for the first cut.

- [ ] **Step 2: Verify typecheck of the package**

Run: `pnpm --filter @multica/views typecheck`
Expected: exit 0 (HealthPill + types resolve).

- [ ] **Step 3: Commit**

```bash
git add packages/views/projects/components/project-update-card.tsx
git commit -m "feat(projects): ProjectUpdateCard component"
```

---

## Task 17: ProjectUpdateComposer component

**Files:**
- Create: `packages/views/projects/components/project-update-composer.tsx`

- [ ] **Step 1: Write the component**

`packages/views/projects/components/project-update-composer.tsx`:
```typescript
import { useState } from "react";
import type { ProjectHealth } from "@multica/core/types/project";
import { useCreateProjectUpdate } from "@multica/core/projects/update-queries";
import { ContentEditor } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

interface ProjectUpdateComposerProps {
  wsId: string;
  projectId: string;
}

const HEALTH_OPTIONS: { value: ProjectHealth; label: string; dot: string }[] = [
  { value: "on_track", label: "On track", dot: "bg-emerald-500" },
  { value: "at_risk", label: "At risk", dot: "bg-amber-500" },
  { value: "off_track", label: "Off track", dot: "bg-red-500" },
];

export function ProjectUpdateComposer({ wsId, projectId }: ProjectUpdateComposerProps) {
  const [health, setHealth] = useState<ProjectHealth>("on_track");
  const [body, setBody] = useState("");
  const [resetKey, setResetKey] = useState(0);
  const createUpdate = useCreateProjectUpdate(wsId, projectId);

  const submit = () => {
    if (createUpdate.isPending) return;
    createUpdate.mutate(
      { health, body },
      {
        onSuccess: () => {
          setBody("");
          setHealth("on_track");
          setResetKey((k) => k + 1);
        },
      },
    );
  };

  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="flex items-center gap-2">
        {HEALTH_OPTIONS.map((opt) => (
          <button
            key={opt.value}
            type="button"
            onClick={() => setHealth(opt.value)}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
              health === opt.value ? "border-foreground" : "border-border text-muted-foreground",
            )}
          >
            <span className={cn("h-2 w-2 rounded-full", opt.dot)} />
            {opt.label}
          </button>
        ))}
      </div>
      <div className="mt-3">
        <ContentEditor
          key={`update-composer-${resetKey}`}
          defaultValue=""
          placeholder="Write a project update…"
          onUpdate={(markdown) => setBody(markdown)}
        />
      </div>
      <div className="mt-3 flex justify-end">
        <Button size="sm" onClick={submit} disabled={createUpdate.isPending}>
          {createUpdate.isPending ? "Posting…" : "Post update"}
        </Button>
      </div>
    </div>
  );
}
```
> **Note:** confirm `Button` import path (grep an existing `packages/views` component importing the shadcn button). `ContentEditor` is exported from `packages/views/editor` per the reference; confirm the relative path from `projects/components/`. If the codebase requires i18n for button/label strings, wire `useT`.

- [ ] **Step 2: Verify typecheck**

Run: `pnpm --filter @multica/views typecheck`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add packages/views/projects/components/project-update-composer.tsx
git commit -m "feat(projects): ProjectUpdateComposer component"
```

---

## Task 18: ProjectUpdatesTab + wire into project detail

**Files:**
- Create: `packages/views/projects/components/project-updates-tab.tsx`
- Test: `packages/views/projects/components/project-updates-tab.test.tsx`
- Modify: `packages/views/projects/components/project-detail.tsx`

- [ ] **Step 1: Write the failing test**

`packages/views/projects/components/project-updates-tab.test.tsx`:
```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ProjectUpdatesTab } from "./project-updates-tab";

vi.mock("@multica/core/api", () => ({
  api: {
    listProjectUpdates: vi.fn().mockResolvedValue({ updates: [], total: 0 }),
    createProjectUpdate: vi.fn(),
  },
}));

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("ProjectUpdatesTab", () => {
  beforeEach(() => vi.clearAllMocks());

  it("shows an empty state when there are no updates", async () => {
    wrap(<ProjectUpdatesTab wsId="ws1" projectId="p1" />);
    expect(await screen.findByText(/no updates yet/i)).toBeInTheDocument();
  });
});
```
> **Note:** match the mocking convention in existing `packages/views/projects/*.test.tsx` (the reference says mock `@multica/core` / `@multica/core/api`, never `next/*`). Adjust the mock target path to whatever the composer/tab actually imports (`@multica/core/projects/update-queries` may need a partial mock too — prefer mocking the lowest-level `api`).

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm --filter @multica/views exec vitest run projects/components/project-updates-tab.test.tsx`
Expected: FAIL (module not found).

- [ ] **Step 3: Write the tab**

`packages/views/projects/components/project-updates-tab.tsx`:
```typescript
import { useQuery } from "@tanstack/react-query";
import { projectUpdatesOptions } from "@multica/core/projects/update-queries";
import { useDeleteProjectUpdate } from "@multica/core/projects/update-queries";
import { ProjectUpdateComposer } from "./project-update-composer";
import { ProjectUpdateCard } from "./project-update-card";

interface ProjectUpdatesTabProps {
  wsId: string;
  projectId: string;
  canModerate?: boolean;
}

export function ProjectUpdatesTab({ wsId, projectId, canModerate }: ProjectUpdatesTabProps) {
  const { data: updates = [], isLoading } = useQuery(projectUpdatesOptions(wsId, projectId));
  const deleteUpdate = useDeleteProjectUpdate(wsId, projectId);

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 p-4">
      <ProjectUpdateComposer wsId={wsId} projectId={projectId} />
      {isLoading ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading…</p>
      ) : updates.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">
          No updates yet. Post the first project update above.
        </p>
      ) : (
        updates.map((u) => (
          <ProjectUpdateCard
            key={u.id}
            update={u}
            canModerate={canModerate}
            onDelete={(id) => deleteUpdate.mutate(id)}
          />
        ))
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm --filter @multica/views exec vitest run projects/components/project-updates-tab.test.tsx`
Expected: 1 passing.

- [ ] **Step 5: Wire the tab into project-detail**

In `packages/views/projects/components/project-detail.tsx`:
- Add imports:
```typescript
import { ProjectUpdatesTab } from "./project-updates-tab";
import { HealthPill } from "./health-pill";
```
- Add a local tab state at the top of the component that renders the main content area (the component that currently renders the Issues views — `ProjectIssuesContent` per the reference, or its parent that owns the header):
```typescript
const [contentTab, setContentTab] = useState<"issues" | "updates">("issues");
```
- Render a segmented control above the content area (next to the existing view switcher, or as a top-level toggle in the project detail header):
```typescript
<div className="flex items-center gap-1 border-b border-border px-3">
  <button
    type="button"
    onClick={() => setContentTab("issues")}
    className={cn(
      "border-b-2 px-3 py-2 text-sm",
      contentTab === "issues" ? "border-foreground text-foreground" : "border-transparent text-muted-foreground",
    )}
  >
    Issues
  </button>
  <button
    type="button"
    onClick={() => setContentTab("updates")}
    className={cn(
      "border-b-2 px-3 py-2 text-sm",
      contentTab === "updates" ? "border-foreground text-foreground" : "border-transparent text-muted-foreground",
    )}
  >
    Updates
  </button>
</div>
```
- Guard the existing content so the Issues toolbar + views render only when `contentTab === "issues"`, and render `<ProjectUpdatesTab wsId={wsId} projectId={projectId} />` when `contentTab === "updates"`. Obtain `wsId` via the existing `useWorkspaceId()` already used in this file (or the prop already threaded for the project).

> **Note:** keep the change minimal — wrap the current issues content block in `{contentTab === "issues" && ( ... )}` and add the updates branch. Do not refactor the existing view-switcher. Confirm `cn` and `useState` are imported.

- [ ] **Step 6: Verify typecheck + tests**

Run: `pnpm --filter @multica/views typecheck && pnpm --filter @multica/views exec vitest run projects/`
Expected: exit 0; project view tests pass.

- [ ] **Step 7: Commit**

```bash
git add packages/views/projects/components/project-updates-tab.tsx packages/views/projects/components/project-updates-tab.test.tsx packages/views/projects/components/project-detail.tsx
git commit -m "feat(projects): Updates tab on project detail"
```

---

## Task 19: Header health pill + dates

**Files:**
- Modify: `packages/views/projects/components/project-detail.tsx`

- [ ] **Step 1: Show health + dates in the project header sidebar**

In the project-detail sidebar (the Properties/Progress area), add, where the project object is in scope:
```typescript
{/* Current health (derived from latest update) */}
<div className="flex items-center justify-between">
  <span className="text-xs text-muted-foreground">Health</span>
  <HealthPill health={project.health} />
</div>

{/* Dates */}
{(project.start_date || project.target_date) && (
  <div className="flex items-center justify-between">
    <span className="text-xs text-muted-foreground">Timeline</span>
    <span className="text-xs text-foreground">
      {formatProjectDates(project.start_date, project.target_date)}
    </span>
  </div>
)}
```
- Add a small helper near the top of the file:
```typescript
function formatProjectDates(start: string | null, target: string | null): string {
  const fmt = (d: string) => new Date(d).toLocaleDateString(undefined, { month: "short", day: "numeric" });
  if (start && target) {
    const daysLeft = Math.ceil((new Date(target).getTime() - Date.now()) / 86_400_000);
    const left = daysLeft >= 0 ? `${daysLeft}d left` : `${Math.abs(daysLeft)}d over`;
    return `${fmt(start)} → ${fmt(target)} · ${left}`;
  }
  if (target) {
    const daysLeft = Math.ceil((new Date(target).getTime() - Date.now()) / 86_400_000);
    return `${fmt(target)} · ${daysLeft >= 0 ? `${daysLeft}d left` : `${Math.abs(daysLeft)}d over`}`;
  }
  return start ? `Started ${fmt(start)}` : "";
}
```
> **Note:** if the sidebar already has a date editor pattern (DatePicker) used elsewhere for issues, prefer reusing it to also make the dates editable. Editing dates is optional for Phase 1 display; making them editable is a nice-to-have — at minimum display them. Match the existing sidebar row styling.

- [ ] **Step 2: Verify typecheck**

Run: `pnpm --filter @multica/views typecheck`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add packages/views/projects/components/project-detail.tsx
git commit -m "feat(projects): show health + timeline in project header"
```

---

## Task 20: Projects list health dot

**Files:**
- Modify: the projects list row component (find it: `packages/views/projects/components/projects-page.tsx` or a `project-row`/`project-card` it renders)

- [ ] **Step 1: Add HealthPill to each project row**

Locate where a project's title/status renders in the list (grep for `project.status` in `packages/views/projects/`). Add next to the status/meta:
```typescript
<HealthPill health={project.health} />
```
with `import { HealthPill } from "./health-pill";` (adjust relative path).

- [ ] **Step 2: Verify typecheck + build the web app**

Run: `pnpm --filter @multica/views typecheck`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add packages/views/projects/
git commit -m "feat(projects): health indicator in projects list"
```

---

## Task 21: Full verification

- [ ] **Step 1: Run the full check suite**

Run: `make check`
Expected: typecheck, TS unit tests, Go tests, and E2E all pass. Fix any failures by reading the output and correcting the offending task's code, then re-run.

- [ ] **Step 2: Manual smoke (optional but recommended)**

Run the app (`make dev`), open a project, post an update with each health value, confirm: the feed updates, the header health pill changes, the projects list reflects the latest health, and dates render. Confirm a second browser/tab receives the update via WS (health refreshes without reload).

- [ ] **Step 3: Final commit if any fixups were needed**

```bash
git add -A
git commit -m "chore(projects): verification fixups for health & updates"
```

---

## Self-Review Notes (coverage map)

- Spec "Add start_date + target_date" → Task 1, Task 7 (API), Task 9 (types), Task 19 (display).
- Spec "project_update table (health, body, author)" → Task 2, Task 3, Task 5.
- Spec "health = latest update, batch-loaded, on ProjectResponse" → Task 3 (`GetLatestUpdatesForProjects`), Task 7.
- Spec "routes /api/projects/{id}/updates" → Task 6.
- Spec "WS events project_update:*" → Task 4, handlers in Task 5, realtime in Task 14.
- Spec "agents can author" → Task 5 (optional author_type/author_id).
- Spec "types, client+schema, query/mutation, realtime" → Tasks 9–14.
- Spec "Updates tab, composer, update card, health pill, header dates" → Tasks 15–19.
- Spec "health dot in projects list" → Task 20.
- Spec "enum drift downgrades not crashes" → Task 15 (HealthPill fallback) + Task 10/11 (schema fallback).
- Spec "malformed-response test" → Task 11.
- Spec "Go tests: CRUD + workspace isolation + validation + derivation" → Task 8.
- Spec "view tests: feed/empty, enum-drift, composer" → Tasks 15, 18.

**Deferred to Phase 2 (out of scope here):** comments + reactions on updates; workspace activity feed; auto-computed health.
```
