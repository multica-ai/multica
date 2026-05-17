# Allow Members to Create Squads — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let any workspace `member` create a Squad, restrict post-create writes to the Squad's creator plus workspace owner/admin, and hide management controls in the UI for non-managers.

**Architecture:**
- Backend: introduce two helpers in `server/internal/handler/squad.go` — a pure predicate `canManageSquad(member, squad)` and a request helper `requireSquadManager(w, r)` that resolves member + squad + permission in one call. Relax `CreateSquad` to any workspace member. Switch the five post-create write endpoints to the new helper.
- Frontend: extend the existing "optional callback = hidden control" convention (currently only `onCreateAgentClick`) to every editable sub-component in the Squad detail page, then introduce a single `canManageSquad` predicate at the page root that decides which callbacks to pass.

**Tech Stack:** Go 1.26 (chi v5, pgx, sqlc generated code), TypeScript / React / TanStack Query (in `packages/views/`), Vitest + testing-library for view tests.

---

## Spec Linkage and Divergence Note

This plan implements `docs/superpowers/specs/2026/05/17/member-can-create-squad-design.md`.

One concrete divergence from the spec:

- **Spec §4.2** claimed the squad-detail sub-components already followed a convention of "callback prop undefined ⇒ control hidden." That is **only true for `onCreateAgentClick`** (`SquadOverviewPane` line 928, `SquadMembersTab` line 1040). Every other management callback (`onRename`, `onUpdateDescription`, `onUploadAvatar`, `onAddMemberClick`, `onSetLeader`, `onRemoveMember`, `onUpdateRole`, `onSaveInstructions`) is currently a *required* prop on its consumer component.
- **Resolution:** This plan extends the same convention to all five leaf editor sub-components and to the two propagating parents. The user-visible behavior is what the spec described; the code surface is a bit larger than the spec implied. No behavior change beyond the spec.

---

## File Map

### Backend (Go)

| Path | Change |
|---|---|
| `server/internal/handler/squad.go` | Add `canManageSquad` predicate + `requireSquadManager` helper. Replace role gates on six endpoints. |
| `server/internal/handler/squad_perm_test.go` | **New** — table-driven unit test for `canManageSquad`; integration tests for all six endpoints exercising member/admin/creator combinations. |

### Frontend (TypeScript / React)

| Path | Change |
|---|---|
| `packages/views/squads/components/squad-detail-page.tsx` | Add `canManageSquad` predicate. Make leaf editors accept optional save callbacks. Propagate optionality through `SquadDetailInspector`, `SquadOverviewPane`, `SquadMembersTab`. Gate prop passing + Archive button at the page root. |
| `packages/views/squads/components/squad-detail-page.test.tsx` | **New** — view-level integration test asserting that management controls render only when caller can manage. |

No DB migrations, no shared package changes, no API client changes.

---

## Conventions This Plan Follows

- **TDD per task** — write the failing test first, see it fail, write the minimal code to pass, see it pass, commit.
- **Each commit is atomic** — one logical change with its tests. Six endpoint switches ⇒ six commits.
- **Type safety preserved** — Go: untouched function signatures except the gate-helper swap. TS: optional props (`prop?: Fn`) replace required ones, no `any`.
- **Commit message format** — `feat(squad): …`, `test(squad): …`, `refactor(squad): …` per CLAUDE.md commit rules.

---

### Task 1: Add `canManageSquad` predicate and `requireSquadManager` helper

**Files:**
- Modify: `server/internal/handler/squad.go` (append helpers at top of file, after the converter section)
- Create: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Write the failing unit test for the pure predicate**

Create `server/internal/handler/squad_perm_test.go` with:

```go
package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidFromBytes(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	return u
}

func TestCanManageSquad(t *testing.T) {
	uA := uuidFromBytes(0xAA)
	uB := uuidFromBytes(0xBB)

	cases := []struct {
		name   string
		member db.Member
		squad  db.Squad
		want   bool
	}{
		{
			name:   "owner who is not the creator",
			member: db.Member{UserID: uA, Role: "owner"},
			squad:  db.Squad{CreatorID: uB},
			want:   true,
		},
		{
			name:   "admin who is not the creator",
			member: db.Member{UserID: uA, Role: "admin"},
			squad:  db.Squad{CreatorID: uB},
			want:   true,
		},
		{
			name:   "plain member who is the creator",
			member: db.Member{UserID: uA, Role: "member"},
			squad:  db.Squad{CreatorID: uA},
			want:   true,
		},
		{
			name:   "plain member who is not the creator",
			member: db.Member{UserID: uA, Role: "member"},
			squad:  db.Squad{CreatorID: uB},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canManageSquad(tc.member, tc.squad); got != tc.want {
				t.Fatalf("canManageSquad: got %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test, expect it to fail**

```
cd server && go test ./internal/handler/ -run TestCanManageSquad
```

Expected: compile error `undefined: canManageSquad`.

- [ ] **Step 3: Add `canManageSquad` and `requireSquadManager` in `squad.go`**

Add the following to `server/internal/handler/squad.go`, inserted right after the existing converter section (i.e. after `squadMemberToResponse`, before `func (h *Handler) ListSquads`):

```go
// ── Authorization helpers ───────────────────────────────────────────────────

// canManageSquad reports whether the caller may mutate this squad.
// Workspace owner/admin always can; otherwise only the squad's creator.
func canManageSquad(member db.Member, squad db.Squad) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return member.UserID == squad.CreatorID
}

// requireSquadManager resolves the calling workspace member, loads the
// target squad, and confirms the caller may manage it. On any failure it
// writes the appropriate HTTP response and returns ok=false — callers
// must return immediately.
func (h *Handler) requireSquadManager(w http.ResponseWriter, r *http.Request) (db.Squad, db.Member, bool) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return db.Squad{}, db.Member{}, false
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return db.Squad{}, db.Member{}, false
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Squad{}, db.Member{}, false
	}
	return squad, member, true
}
```

- [ ] **Step 4: Run the test, expect it to pass**

```
cd server && go test ./internal/handler/ -run TestCanManageSquad -v
```

Expected: `PASS` for all four sub-cases. No usages of the new helpers yet — that's intentional, they'll be wired up in subsequent tasks. Go does not warn about unused package-level functions.

- [ ] **Step 5: Run typecheck for the package**

```
cd server && go build ./internal/handler/
```

Expected: clean build, no errors.

- [ ] **Step 6: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): add canManageSquad predicate and requireSquadManager helper"
```

---

### Task 2: Relax `CreateSquad` to any workspace member

**Files:**
- Modify: `server/internal/handler/squad.go:117-191` (the `CreateSquad` function — only line 119 changes)
- Modify: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Add the failing integration test**

Append to `server/internal/handler/squad_perm_test.go`:

```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// createPlainMember inserts a new user and adds them to testWorkspaceID
// as a workspace `member`. Returns the new user's UUID. Registers
// cleanup that deletes the user (cascade removes the membership row).
func createPlainMember(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	email := label + "@multica.test"

	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, label, email).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", label, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add %s as member: %v", label, err)
	}
	return userID
}

func TestCreateSquad_MemberCanCreate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	memberID := createPlainMember(t, "squad-creator-member")
	leaderID := createHandlerTestAgent(t, "squad-create-leader", []byte(`{}`))

	body := map[string]any{"name": "MemberSquad", "leader_id": leaderID}
	req := newRequestAs(memberID, http.MethodPost, "/api/squads", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad as plain member: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode squad response: %v", err)
	}
	if resp.CreatorID != memberID {
		t.Fatalf("creator_id: got %s, want %s", resp.CreatorID, memberID)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
}

func TestCreateSquad_OwnerCanStillCreate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "squad-create-leader-owner", []byte(`{}`))

	body := map[string]any{"name": "OwnerSquad", "leader_id": leaderID}
	req := newRequest(http.MethodPost, "/api/squads", body) // testUserID is workspace owner
	req = withURLParam(req, "workspaceId", testWorkspaceID)

	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CreateSquad as owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp SquadResponse
	json.NewDecoder(w.Body).Decode(&resp)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, resp.ID)
	})
}
```

Note: `newRequestAs` already exists in `server/internal/handler/agent_access_test.go:120`. Since both files are in package `handler`, the symbol is reachable without re-declaration.

- [ ] **Step 2: Run the new tests, expect `TestCreateSquad_MemberCanCreate` to fail**

```
cd server && go test ./internal/handler/ -run 'TestCreateSquad_' -v
```

Expected:
- `TestCreateSquad_MemberCanCreate` → **FAIL** with `expected 201, got 403`.
- `TestCreateSquad_OwnerCanStillCreate` → PASS (regression baseline).

- [ ] **Step 3: Relax `CreateSquad`**

In `server/internal/handler/squad.go`, find this block at line 117-122:

```go
func (h *Handler) CreateSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}
```

Replace the role check with a plain member check:

```go
func (h *Handler) CreateSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
```

Nothing else in the function changes — `member` is still used as `CreatorID` on line 172.

- [ ] **Step 4: Run the tests, expect them to pass**

```
cd server && go test ./internal/handler/ -run 'TestCreateSquad_' -v
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): allow plain workspace members to create squads"
```

---

### Task 3: Switch `UpdateSquad` to `requireSquadManager`

**Files:**
- Modify: `server/internal/handler/squad.go:201-274` (the `UpdateSquad` function — lines 202-210 change)
- Modify: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Add a shared fixture for member-owned squads**

Append to `server/internal/handler/squad_perm_test.go`:

```go
// memberOwnedSquadFixture creates two plain workspace members and a
// squad owned by the first. Returns (squadID, creatorID, plainMemberID,
// leaderAgentID). All four entities are cleaned up on test teardown.
func memberOwnedSquadFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	ctx := context.Background()
	creatorID := createPlainMember(t, "squad-owner-member")
	otherID := createPlainMember(t, "squad-bystander-member")
	leaderID := createHandlerTestAgent(t, "squad-fixture-leader", []byte(`{}`))

	body := map[string]any{"name": "FixtureSquad", "leader_id": leaderID}
	req := newRequestAs(creatorID, http.MethodPost, "/api/squads", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateSquad(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("fixture CreateSquad: got %d: %s", w.Code, w.Body.String())
	}
	var resp SquadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("fixture decode squad: %v", err)
	}
	squadID := resp.ID
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID, creatorID, otherID, leaderID
}
```

This depends on `CreateSquad` accepting members (Task 2). Do not move this helper into Task 1 — it would fail until Task 2 lands.

- [ ] **Step 2: Add the failing tests for `UpdateSquad`**

Append to `server/internal/handler/squad_perm_test.go`:

```go
func TestUpdateSquad_CreatorMemberCanUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, _, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"name": "Renamed"}
	req := newRequestAs(creatorID, http.MethodPut, "/api/squads/"+squadID, body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as creator-member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquad_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"name": "Hijacked"}
	req := newRequestAs(otherID, http.MethodPut, "/api/squads/"+squadID, body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSquad as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquad_AdminCanUpdateOthersSquad(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, _, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"name": "AdminEdit"}
	req := newRequest(http.MethodPut, "/api/squads/"+squadID, body) // testUserID is owner
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquad(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquad as workspace owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 3: Run the tests, expect `TestUpdateSquad_CreatorMemberCanUpdate` to fail**

```
cd server && go test ./internal/handler/ -run 'TestUpdateSquad_' -v
```

Expected:
- `TestUpdateSquad_CreatorMemberCanUpdate` → **FAIL** with `expected 200, got 403`.
- `TestUpdateSquad_NonCreatorMemberForbidden` → PASS (member is still rejected by the old role gate, so the assertion of 403 already holds).
- `TestUpdateSquad_AdminCanUpdateOthersSquad` → PASS.

- [ ] **Step 4: Switch `UpdateSquad` to `requireSquadManager`**

In `server/internal/handler/squad.go`, replace lines 201-210 (the function header through the `loadSquadInWorkspace` call) with:

```go
func (h *Handler) UpdateSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squad, _, ok := h.requireSquadManager(w, r)
	if !ok {
		return
	}
```

That is, drop the `requireWorkspaceRole` block entirely **and** drop the standalone `loadSquadInWorkspace` call (since `requireSquadManager` does both). The rest of `UpdateSquad` is unchanged — `workspaceID` is still used downstream on line 211 and beyond, and `squad.ID` continues to be the target of `UpdateSquadParams`.

- [ ] **Step 5: Run the tests, expect all three to pass**

```
cd server && go test ./internal/handler/ -run 'TestUpdateSquad_' -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): allow squad creator to update their own squad"
```

---

### Task 4: Switch `DeleteSquad` to `requireSquadManager`

**Files:**
- Modify: `server/internal/handler/squad.go:276-316`
- Modify: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Add failing tests**

Append:

```go
func TestDeleteSquad_CreatorMemberCanDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, _, _ := memberOwnedSquadFixture(t)

	req := newRequestAs(creatorID, http.MethodDelete, "/api/squads/"+squadID, nil)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.DeleteSquad(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSquad as creator-member: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteSquad_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)

	req := newRequestAs(otherID, http.MethodDelete, "/api/squads/"+squadID, nil)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.DeleteSquad(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteSquad as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run, expect first test to fail**

```
cd server && go test ./internal/handler/ -run 'TestDeleteSquad_' -v
```

Expected: `TestDeleteSquad_CreatorMemberCanDelete` FAILs (`expected 204, got 403`); `TestDeleteSquad_NonCreatorMemberForbidden` PASSes.

- [ ] **Step 3: Switch `DeleteSquad`**

In `server/internal/handler/squad.go`, replace lines 276-285 (function header through `loadSquadInWorkspace`) with:

```go
func (h *Handler) DeleteSquad(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squad, _, ok := h.requireSquadManager(w, r)
	if !ok {
		return
	}
```

The rest of the function (`squad.ArchivedAt.Valid` check, the transfer-assignees call, the archive call, the publish) is unchanged. `workspaceID` is still used on line 311 for the publish call.

- [ ] **Step 4: Run, expect pass**

```
cd server && go test ./internal/handler/ -run 'TestDeleteSquad_' -v
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): allow squad creator to delete their own squad"
```

---

### Task 5: Switch `AddSquadMember` to `requireSquadManager`

**Files:**
- Modify: `server/internal/handler/squad.go:337-411`
- Modify: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Add failing tests**

Append. The fixture's `otherID` is a workspace member not in the squad, perfect for adding.

```go
func TestAddSquadMember_CreatorMemberCanAdd(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, otherID, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"member_type": "member", "member_id": otherID}
	req := newRequestAs(creatorID, http.MethodPost, "/api/squads/"+squadID+"/members", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.AddSquadMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("AddSquadMember as creator-member: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddSquadMember_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)

	body := map[string]any{"member_type": "member", "member_id": otherID}
	// otherID is the caller AND the proposed member — server rejects on perm
	// before it even reads the body, so this is fine.
	req := newRequestAs(otherID, http.MethodPost, "/api/squads/"+squadID+"/members", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.AddSquadMember(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("AddSquadMember as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run, expect first test to fail**

```
cd server && go test ./internal/handler/ -run 'TestAddSquadMember_' -v
```

Expected: `TestAddSquadMember_CreatorMemberCanAdd` FAILs; the other PASSes.

- [ ] **Step 3: Switch `AddSquadMember`**

In `server/internal/handler/squad.go`, replace lines 337-346 with:

```go
func (h *Handler) AddSquadMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squad, _, ok := h.requireSquadManager(w, r)
	if !ok {
		return
	}
```

The rest of the function (parsing `wsUUID`, decoding the body, the role validation, `Queries.AddSquadMember`, publish) is unchanged. `workspaceID` is still used downstream.

- [ ] **Step 4: Run, expect pass**

```
cd server && go test ./internal/handler/ -run 'TestAddSquadMember_' -v
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): allow squad creator to add members"
```

---

### Task 6: Switch `RemoveSquadMember` to `requireSquadManager`

**Files:**
- Modify: `server/internal/handler/squad.go:413-462`
- Modify: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Add failing tests**

Append. We add `otherID` to the squad first (via direct SQL to avoid coupling this test to Task 5), then verify removal perms.

```go
// addSquadMemberDirect inserts a squad membership row directly via SQL to
// avoid going through the AddSquadMember endpoint. Used by tests that
// need a pre-populated member without exercising the endpoint they are
// about to test.
func addSquadMemberDirect(t *testing.T, squadID, memberID, memberType, role string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, $2, $3, $4)
	`, squadID, memberType, memberID, role); err != nil {
		t.Fatalf("addSquadMemberDirect: %v", err)
	}
}

func TestRemoveSquadMember_CreatorMemberCanRemove(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID}
	req := newRequestAs(creatorID, http.MethodDelete, "/api/squads/"+squadID+"/members", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.RemoveSquadMember(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("RemoveSquadMember as creator-member: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRemoveSquadMember_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID}
	req := newRequestAs(otherID, http.MethodDelete, "/api/squads/"+squadID+"/members", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.RemoveSquadMember(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("RemoveSquadMember as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run, expect first to fail**

```
cd server && go test ./internal/handler/ -run 'TestRemoveSquadMember_' -v
```

Expected: `TestRemoveSquadMember_CreatorMemberCanRemove` FAILs; the other PASSes.

- [ ] **Step 3: Switch `RemoveSquadMember`**

In `server/internal/handler/squad.go`, replace lines 413-422 with:

```go
func (h *Handler) RemoveSquadMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squad, _, ok := h.requireSquadManager(w, r)
	if !ok {
		return
	}
```

Everything else (body decoding, leader-removal guard, `Queries.RemoveSquadMember`, publish) is unchanged. `workspaceID` is still used in the publish call.

- [ ] **Step 4: Run, expect pass**

```
cd server && go test ./internal/handler/ -run 'TestRemoveSquadMember_' -v
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): allow squad creator to remove members"
```

---

### Task 7: Switch `UpdateSquadMemberRole` to `requireSquadManager`

**Files:**
- Modify: `server/internal/handler/squad.go:464-` (the `UpdateSquadMemberRole` function — lines 465-473 change)
- Modify: `server/internal/handler/squad_perm_test.go`

- [ ] **Step 1: Add failing tests**

Append:

```go
func TestUpdateSquadMemberRole_CreatorMemberCanUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, creatorID, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID, "role": "contributor"}
	req := newRequestAs(creatorID, http.MethodPatch, "/api/squads/"+squadID+"/members/role", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquadMemberRole(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateSquadMemberRole as creator-member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSquadMemberRole_NonCreatorMemberForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	squadID, _, otherID, _ := memberOwnedSquadFixture(t)
	addSquadMemberDirect(t, squadID, otherID, "member", "member")

	body := map[string]any{"member_type": "member", "member_id": otherID, "role": "contributor"}
	req := newRequestAs(otherID, http.MethodPatch, "/api/squads/"+squadID+"/members/role", body)
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	req = withURLParam(req, "id", squadID)

	w := httptest.NewRecorder()
	testHandler.UpdateSquadMemberRole(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateSquadMemberRole as bystander-member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run, expect first to fail**

```
cd server && go test ./internal/handler/ -run 'TestUpdateSquadMemberRole_' -v
```

Expected: `TestUpdateSquadMemberRole_CreatorMemberCanUpdate` FAILs; the other PASSes.

- [ ] **Step 3: Switch `UpdateSquadMemberRole`**

In `server/internal/handler/squad.go`, replace lines 464-473 with:

```go
func (h *Handler) UpdateSquadMemberRole(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squad, _, ok := h.requireSquadManager(w, r)
	if !ok {
		return
	}
```

The rest of the function is unchanged. `workspaceID` is still used downstream on line 501 in the `h.publish` call, so the local variable does not become unused.

- [ ] **Step 4: Run, expect pass**

```
cd server && go test ./internal/handler/ -run 'TestUpdateSquadMemberRole_' -v
```

Expected: both PASS.

- [ ] **Step 5: Run the full handler test suite to catch any regression**

```
cd server && go test ./internal/handler/
```

Expected: all PASS (other tests that pre-date this work should be unaffected because `requireWorkspaceRole` and `loadSquadInWorkspace` still exist; we just stopped calling them from squad handlers).

- [ ] **Step 6: Commit**

```
git add server/internal/handler/squad.go server/internal/handler/squad_perm_test.go
git commit -m "feat(squad): allow squad creator to update member roles"
```

---

### Task 8: Make leaf editor sub-components accept optional callbacks

**Files:**
- Modify: `packages/views/squads/components/squad-detail-page.tsx`

The five leaf editors (`SquadAvatarEditor`, `SquadNameEditor`, `SquadDescriptionEditor`, `RoleEditor`, `SquadInstructionsTab`) currently require their save callback. After this task, each renders a non-interactive fallback when its save callback is omitted, mirroring how `SquadMembersTab` already hides the "Create Agent" button when `onCreateAgentClick` is undefined.

Locate `RoleEditor` first — its line number isn't in the file map above; grep before editing.

- [ ] **Step 1: Locate `RoleEditor` and `SquadInstructionsTab`**

```
grep -n "function RoleEditor\|function SquadInstructionsTab" packages/views/squads/components/squad-detail-page.tsx
```

Note the line numbers for the next steps.

- [ ] **Step 2: Make `SquadAvatarEditor.onUpload` optional and render the avatar as a non-button when omitted**

In `packages/views/squads/components/squad-detail-page.tsx`, locate the `SquadAvatarEditor` function (starts around line 296). Change its props signature so `onUpload` is optional:

```tsx
function SquadAvatarEditor({
  squad,
  initials,
  uploading,
  onUpload,
}: {
  squad: Squad;
  initials: string;
  uploading: boolean;
  onUpload?: (url: string) => Promise<unknown>;
}) {
```

Then, near the top of the function body (after the existing `const { upload, uploading: fileUploading } = useFileUpload(api);` line), add a read-only short-circuit that renders the avatar as a plain `<div>` (not a `<button>`) and skips the file input entirely when no upload callback is supplied:

```tsx
  if (!onUpload) {
    return (
      <div
        className="h-16 w-16 shrink-0 overflow-hidden rounded-lg bg-muted"
        aria-label="Squad avatar"
      >
        {squad.avatar_url ? (
          <ActorAvatarBase
            name={squad.name}
            initials={initials}
            avatarUrl={squad.avatar_url}
            size={64}
            className="rounded-none"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-muted-foreground">
            <Users className="h-7 w-7" />
          </div>
        )}
      </div>
    );
  }
```

The existing editable branch below is unchanged. Inside the editable branch the existing `await onUpload(result.link);` call is fine because that branch only runs when `onUpload` is defined.

- [ ] **Step 3: Make `SquadNameEditor.onSave` optional and render plain text when omitted**

Inside `SquadNameEditor` (starts around line 369). Change the prop to optional:

```tsx
}: {
  value: string;
  onSave?: (next: string) => Promise<void>;
}) {
```

Add a read-only branch at the top of the function body, before the editable JSX:

```tsx
  if (!onSave) {
    return (
      <div className="text-base font-medium leading-tight">{value}</div>
    );
  }
```

(Keep the same wrapping element and styles the editable branch uses for the displayed name, so the layout doesn't shift.)

- [ ] **Step 4: Make `SquadDescriptionEditor.onSave` optional and render plain text when omitted**

Inside `SquadDescriptionEditor` (starts around line 798). Change the prop to optional:

```tsx
}: {
  value: string;
  onSave?: (next: string) => Promise<void>;
}) {
```

Add a read-only branch at the top:

```tsx
  if (!onSave) {
    return (
      <div className="text-xs text-muted-foreground">
        {value || ""}
      </div>
    );
  }
```

- [ ] **Step 5: Make `RoleEditor.onSave` optional**

`RoleEditor` is used inside `SquadMembersTab` to edit the per-member role inline. Locate it with the grep from Step 1.

Change its `onSave` prop to optional:

```tsx
  onSave?: (next: string) => Promise<void>;
```

Add a read-only branch at the top that renders the current `value` as plain text (or "—" if empty):

```tsx
  if (!onSave) {
    return (
      <div className="text-xs text-muted-foreground">{value || "—"}</div>
    );
  }
```

- [ ] **Step 6: Make `SquadInstructionsTab.onSave` optional**

Locate `SquadInstructionsTab` (around line 1169). Change `onSave` to optional:

```tsx
}: {
  squad: Squad;
  onSave?: (instructions: string) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
```

Add a read-only branch that uses the existing `ContentEditor` in read-only mode and omits the Save button:

```tsx
  if (!onSave) {
    return (
      <div className="flex flex-col gap-3">
        <ContentEditor value={squad.instructions ?? ""} readOnly />
      </div>
    );
  }
```

If `ContentEditor` does not currently accept a `readOnly` prop, fall back to rendering `<div className="text-sm whitespace-pre-wrap">{squad.instructions ?? ""}</div>` instead. Check the `ContentEditor` props before deciding:

```
grep -n "readOnly\|disabled" packages/views/editor/content-editor.tsx | head
```

- [ ] **Step 7: Run typecheck**

```
pnpm typecheck
```

Expected: clean. The required-callback callsites in `SquadDetailPage` (lines 225-240) still type-check because passing a defined function to an optional prop is always valid.

- [ ] **Step 8: Commit**

```
git add packages/views/squads/components/squad-detail-page.tsx
git commit -m "refactor(squads): make leaf editor callbacks optional for read-only mode"
```

---

### Task 9: Propagate optional callbacks through Inspector / Overview / Members parents

**Files:**
- Modify: `packages/views/squads/components/squad-detail-page.tsx`

This task changes only types and propagation — it does **not** introduce any new gating decision yet. Every callsite still passes a defined callback after this task; only the prop *types* relax.

- [ ] **Step 1: Make `SquadDetailInspector` callbacks optional**

Inside `SquadDetailInspector` (starts around line 706). Change its prop types:

```tsx
}: {
  squad: Squad;
  memberCount: number;
  leaderName: string;
  creatorName: string;
  uploadingAvatar: boolean;
  onUploadAvatar?: (url: string) => Promise<unknown>;
  onRename?: (next: string) => Promise<void>;
  onUpdateDescription?: (next: string) => Promise<void>;
}) {
```

No other change inside `SquadDetailInspector` — it passes these props straight through to `SquadAvatarEditor.onUpload`, `SquadNameEditor.onSave`, and `SquadDescriptionEditor.onSave`, all of which now accept `undefined` (Task 8).

- [ ] **Step 2: Make `SquadOverviewPane` management callbacks optional**

Inside `SquadOverviewPane` (starts around line 907). Change its prop types:

```tsx
}: {
  squad: Squad;
  members: SquadMember[];
  isLeader: (m: SquadMember) => boolean;
  getEntityName: (type: string, id: string) => string;
  onAddMemberClick?: () => void;
  // existing onCreateAgentClick already optional
  onCreateAgentClick?: () => void;
  onSetLeader?: (agentId: string) => void;
  onRemoveMember?: (m: SquadMember) => void;
  onUpdateRole?: (m: SquadMember, role: string) => Promise<void>;
  onSaveInstructions?: (next: string) => Promise<void>;
  setLeaderPending: boolean;
}) {
```

These callbacks are forwarded to `SquadMembersTab` and `SquadInstructionsTab` unchanged. No body change.

- [ ] **Step 3: Make `SquadMembersTab` callbacks optional and hide controls when absent**

`SquadMembersTab` (starts around line 1024). Change its prop types:

```tsx
}: {
  members: SquadMember[];
  isLeader: (m: SquadMember) => boolean;
  getEntityName: (type: string, id: string) => string;
  onAddMemberClick?: () => void;
  onCreateAgentClick?: () => void;
  onSetLeader?: (agentId: string) => void;
  onRemoveMember?: (m: SquadMember) => void;
  onUpdateRole?: (m: SquadMember, role: string) => Promise<void>;
  setLeaderPending: boolean;
}) {
```

Then update the JSX so each button only renders when its callback is defined:

- The "Add member" button on line 1064 currently calls `onAddMemberClick` unconditionally. Wrap it:
  ```tsx
  {onAddMemberClick && (
    <Button size="sm" variant="outline" onClick={onAddMemberClick}>
      <Plus className="size-3.5 mr-1.5" />
      {t(($) => $.members_tab.add_member_button)}
    </Button>
  )}
  ```
- The "Make leader" Crown button block (around line 1117-1137) is currently `m.member_type === "agent" && !isLeader(m) && (...)`. Add `onSetLeader && ` to that condition:
  ```tsx
  {onSetLeader && m.member_type === "agent" && !isLeader(m) && (
    <Tooltip>...</Tooltip>
  )}
  ```
- The "Remove" Trash2 button block (around line 1138-1157) is currently `!isLeader(m) && (...)`. Add `onRemoveMember && `:
  ```tsx
  {onRemoveMember && !isLeader(m) && (
    <Tooltip>...</Tooltip>
  )}
  ```
- The `RoleEditor` on line 1093-1096 currently always passes an inline `async (next) => { await onUpdateRole(m, next); }`. Pass `undefined` when `onUpdateRole` itself is undefined, so `RoleEditor` falls into its read-only branch (Task 8 Step 5):
  ```tsx
  <RoleEditor
    value={m.role ?? ""}
    onSave={
      onUpdateRole
        ? async (next) => { await onUpdateRole(m, next); }
        : undefined
    }
  />
  ```

- [ ] **Step 4: Run typecheck**

```
pnpm typecheck
```

Expected: clean. `SquadDetailPage` still passes concrete callbacks today, so nothing breaks until Task 10 introduces conditional passing.

- [ ] **Step 5: Commit**

```
git add packages/views/squads/components/squad-detail-page.tsx
git commit -m "refactor(squads): make parent callbacks optional and hide controls when absent"
```

---

### Task 10: Gate at `SquadDetailPage` + add view-level test

**Files:**
- Modify: `packages/views/squads/components/squad-detail-page.tsx`
- Create: `packages/views/squads/components/squad-detail-page.test.tsx`

- [ ] **Step 1: Write the failing view test**

Create `packages/views/squads/components/squad-detail-page.test.tsx`:

```tsx
// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, MemberWithUser, Squad, SquadMember } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSquads from "../../locales/en/squads.json";

const TEST_RESOURCES = {
  en: { common: enCommon, squads: enSquads },
};

const OWNER_USER = "user-owner";
const CREATOR_USER = "user-creator";
const BYSTANDER_USER = "user-bystander";

const fakeSquad: Squad = {
  id: "squad-1",
  workspace_id: "ws-1",
  name: "FakeSquad",
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "agent-1",
  creator_id: CREATOR_USER,
  created_at: "2026-05-17T00:00:00Z",
  updated_at: "2026-05-17T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const fakeMembers: SquadMember[] = [
  { id: "sm-1", squad_id: "squad-1", member_type: "agent", member_id: "agent-1", role: "leader", created_at: "2026-05-17T00:00:00Z" },
];

const fakeWsMembers: MemberWithUser[] = [
  { user_id: OWNER_USER,     workspace_id: "ws-1", role: "owner",  name: "Owner",     avatar_url: null, email: "o@x", created_at: "2026-05-17T00:00:00Z" },
  { user_id: CREATOR_USER,   workspace_id: "ws-1", role: "member", name: "Creator",   avatar_url: null, email: "c@x", created_at: "2026-05-17T00:00:00Z" },
  { user_id: BYSTANDER_USER, workspace_id: "ws-1", role: "member", name: "Bystander", avatar_url: null, email: "b@x", created_at: "2026-05-17T00:00:00Z" },
];

const fakeAgents: Agent[] = [
  { id: "agent-1", workspace_id: "ws-1", name: "LeaderAgent", description: "", visibility: "private", avatar_url: null, runtime_id: "rt-1", runtime_mode: "cloud", runtime_config: {}, owner_id: CREATOR_USER, instructions: "", custom_env: {}, custom_args: [], mcp_config: [], created_at: "2026-05-17T00:00:00Z", updated_at: "2026-05-17T00:00:00Z", archived_at: null, max_concurrent_tasks: 1 } as Agent,
];

const mocks = vi.hoisted(() => ({
  authUserId: "user-owner",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    const k = Array.isArray(key) ? key.join("/") : "";
    if (k.includes("squads") && k.endsWith("members")) return { data: fakeMembers, refetch: () => {} };
    if (k.includes("squads") && k.includes("squad-1")) return { data: fakeSquad, refetch: () => {} };
    if (k.includes("agents")) return { data: fakeAgents };
    if (k.includes("members")) return { data: fakeWsMembers };
    return { data: [], refetch: () => {} };
  },
  useMutation: () => ({ mutate: () => {}, mutateAsync: async () => {}, isPending: false }),
  useQueryClient: () => ({ setQueryData: () => {}, invalidateQueries: () => {} }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getSquad: vi.fn(),
    listSquadMembers: vi.fn(),
    updateSquad: vi.fn(),
    deleteSquad: vi.fn(),
    addSquadMember: vi.fn(),
    removeSquadMember: vi.fn(),
    updateSquadMemberRole: vi.fn(),
    createAgent: vi.fn(),
    uploadFile: vi.fn(),
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: mocks.authUserId } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: vi.fn(), uploading: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "test-ws" }),
  useWorkspacePaths: () => ({
    squads: () => "/test-ws/squads",
    squadDetail: (id: string) => `/test-ws/squads/${id}`,
    agentDetail: (id: string) => `/test-ws/agents/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { squads: (id: string) => ["squads", id], agents: () => ["agents"] },
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@multica/core/utils", () => ({
  isImeComposing: () => false,
  timeAgo: () => "just now",
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ pathname: "/test-ws/squads/squad-1", push: vi.fn() }),
  AppLink: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span />,
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Render the page directly to verify gating.
import { SquadDetailPage } from "./squad-detail-page";

const renderPage = () =>
  render(
    <I18nProvider resources={TEST_RESOURCES} fallbackLocale="en">
      <SquadDetailPage />
    </I18nProvider>
  );

describe("SquadDetailPage gating", () => {
  beforeEach(() => {
    mocks.authUserId = OWNER_USER;
  });

  it("shows the archive button for workspace owner viewing a member-created squad", () => {
    mocks.authUserId = OWNER_USER;
    renderPage();
    expect(screen.getByRole("button", { name: /archive/i })).toBeTruthy();
  });

  it("shows the archive button for the squad creator (a plain member)", () => {
    mocks.authUserId = CREATOR_USER;
    renderPage();
    expect(screen.getByRole("button", { name: /archive/i })).toBeTruthy();
  });

  it("hides the archive button for a plain member who is not the creator", () => {
    mocks.authUserId = BYSTANDER_USER;
    renderPage();
    expect(screen.queryByRole("button", { name: /archive/i })).toBeNull();
  });

  it("hides the add-member button for a plain member who is not the creator", () => {
    mocks.authUserId = BYSTANDER_USER;
    renderPage();
    expect(screen.queryByRole("button", { name: /add member/i })).toBeNull();
  });
});
```

Adjust the regex names if the locale `squads.json` uses different button labels — grep `add_member_button` / `archive_button` in `packages/views/locales/en/squads.json` and use the actual English string.

- [ ] **Step 2: Run the test, expect failures**

```
pnpm --filter @multica/views exec vitest run squads/components/squad-detail-page.test.tsx
```

Expected: the "hides" cases FAIL because the current `SquadDetailPage` always renders the archive and add-member buttons regardless of caller identity.

- [ ] **Step 3: Add `canManageSquad` predicate and gate prop passing**

In `packages/views/squads/components/squad-detail-page.tsx`, inside `SquadDetailPage`, locate the `isWorkspaceAdmin` declaration on line 96. Add immediately below it:

```tsx
  const canManageSquad =
    isWorkspaceAdmin || squad?.creator_id === currentUser?.id;
```

(Use optional chaining on `squad` because the early-return `if (!squad)` on line 184 is below this line — the predicate is computed eagerly with React's normal evaluation order. When `squad` is undefined, the predicate evaluates to `false`, which is safe.)

- [ ] **Step 4: Gate the `Archive` button at the page header**

Replace lines 209-212 in `packages/views/squads/components/squad-detail-page.tsx`:

```tsx
        {canManageSquad && (
          <Button
            size="sm"
            variant="ghost"
            className="text-destructive hover:text-destructive"
            onClick={() => {
              if (confirm("Archive this squad? Issues will be transferred to the leader.")) deleteMut.mutate();
            }}
          >
            <Trash2 className="size-3.5 mr-1 mr-1" />
            {t(($) => $.inspector.archive_button)}
          </Button>
        )}
```

(Wrap the existing JSX, do not duplicate the className/onClick — copy them across exactly as they appear today.)

- [ ] **Step 5: Gate the props passed to `SquadDetailInspector`**

Replace lines 219-228 with:

```tsx
        <SquadDetailInspector
          squad={squad}
          memberCount={members.length}
          leaderName={getEntityName("agent", squad.leader_id)}
          creatorName={getEntityName("member", squad.creator_id)}
          uploadingAvatar={updateSquadMut.isPending}
          onUploadAvatar={canManageSquad ? (url) => updateSquadMut.mutateAsync({ avatar_url: url }) : undefined}
          onRename={canManageSquad ? async (next) => { await updateSquadMut.mutateAsync({ name: next.trim() }); } : undefined}
          onUpdateDescription={canManageSquad ? async (next) => { await updateSquadMut.mutateAsync({ description: next }); } : undefined}
        />
```

- [ ] **Step 6: Gate the props passed to `SquadOverviewPane`**

Replace lines 230-242 with:

```tsx
        <SquadOverviewPane
          squad={squad}
          members={members}
          isLeader={isLeader}
          getEntityName={getEntityName}
          onAddMemberClick={canManageSquad ? () => setShowAddMember(true) : undefined}
          onCreateAgentClick={isWorkspaceAdmin ? () => setShowCreateAgent(true) : undefined}
          onSetLeader={canManageSquad ? (id) => setLeaderMut.mutate(id) : undefined}
          onRemoveMember={canManageSquad ? (m) => removeMemberMut.mutate(m) : undefined}
          onUpdateRole={canManageSquad ? async (m, role) => { await updateRoleMut.mutateAsync({ member: m, role }); } : undefined}
          onSaveInstructions={canManageSquad ? async (next) => { await updateSquadMut.mutateAsync({ instructions: next }); toast.success("Instructions saved"); } : undefined}
          setLeaderPending={setLeaderMut.isPending}
        />
```

Note `onCreateAgentClick` keeps its existing `isWorkspaceAdmin` gate — creating an agent is a workspace-level governance action and is independent of squad ownership (per spec §4.2).

- [ ] **Step 7: Run the view test, expect pass**

```
pnpm --filter @multica/views exec vitest run squads/components/squad-detail-page.test.tsx
```

Expected: all four cases PASS.

- [ ] **Step 8: Run the broader frontend suite to catch regressions**

```
pnpm --filter @multica/views test
```

Expected: clean. The existing `packages/views/modals/create-squad.test.tsx` is unaffected because we only touched the detail page.

- [ ] **Step 9: Run typecheck across the workspace**

```
pnpm typecheck
```

Expected: clean.

- [ ] **Step 10: Commit**

```
git add packages/views/squads/components/squad-detail-page.tsx packages/views/squads/components/squad-detail-page.test.tsx
git commit -m "feat(squads): hide management controls when caller cannot manage the squad"
```

---

## Final Verification

After Task 10 is committed, run the full check pipeline once to confirm nothing else regressed:

- [ ] `pnpm typecheck`
- [ ] `pnpm test`
- [ ] `cd server && go test ./internal/handler/`

If all three pass, the implementation is complete. The end-to-end flow described in the spec is now in effect:

- A plain workspace `member` can open the "Create Squad" dialog and submit successfully.
- They land on the detail page and see all management controls because they are the creator.
- Other members visiting that squad's page see a read-only view (no rename, no avatar upload, no add member, no archive).
- Workspace owners and admins continue to see everything regardless of authorship.
