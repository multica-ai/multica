# Allow Regular Members to Create Squads

**Status:** Draft — design approved, awaiting implementation plan.
**Date:** 2026-05-17

## 1. Background and Problem

Today, the six write endpoints for the Squad resource are gated to workspace
`owner` and `admin` only:

| Endpoint            | Location                                  |
| ------------------- | ----------------------------------------- |
| `CreateSquad`         | `server/internal/handler/squad.go:119`    |
| `UpdateSquad`         | `server/internal/handler/squad.go:203`    |
| `DeleteSquad`         | `server/internal/handler/squad.go:278`    |
| `AddSquadMember`      | `server/internal/handler/squad.go:339`    |
| `RemoveSquadMember`   | `server/internal/handler/squad.go:415`    |
| `UpdateSquadMemberRole` | `server/internal/handler/squad.go:466`  |

A regular workspace `member` who opens the "Create Squad" dialog and submits
gets `403 insufficient permissions` from
`server/internal/handler/handler.go:406`.

This is an outlier in the codebase. `Issue`, `Project`, `Skill`, and `Agent`
mutation endpoints are all reachable by `member`. Squad is the only
first-class resource still locked to admins.

Product position: any workspace member should be able to create a Squad. The
write operations that follow creation should be governed by per-Squad
ownership, not by global workspace role.

## 2. Goals and Non-Goals

### Goals

- A `member` can call `CreateSquad` and succeed.
- For the five post-creation write endpoints (`UpdateSquad`, `DeleteSquad`,
  `AddSquadMember`, `RemoveSquadMember`, `UpdateSquadMemberRole`),
  authorization is:
  - workspace `owner` or `admin`, **or**
  - the user whose `user_id` matches `squads.creator_id`.
- The Squad detail UI hides management controls when the current caller is
  not authorized, so a non-creator non-admin member never sees buttons that
  would fail with 403.

### Non-Goals

- No new "human Squad leader" authorization concept. The Squad members table
  has a `role` column whose value `"leader"` today only carries the
  Leader Agent (added automatically at create time); we are not extending its
  meaning to humans.
- No creator transfer flow. If an owner needs to take over a Squad whose
  creator has left, they can already act through their workspace role; an
  explicit reassignment UI is out of scope.
- No per-member quota on Squad creation.
- No changes to read endpoints (`ListSquads`, `GetSquad`,
  `ListSquadMembers`). They already accept any workspace member via the
  middleware membership check and stay that way.
- No changes to agent, runtime, notification, or activity-log behavior.
- No feature flag, no migration, no API contract change.

## 3. Backend Design

### 3.1 Authorization predicate

Add the following helpers inside `server/internal/handler/squad.go` (kept
local because they are tied to Squad semantics and do not generalize):

```go
// canManageSquad reports whether the caller may mutate this squad.
// Workspace owner/admin always can; otherwise only the squad's creator.
func canManageSquad(member db.Member, squad db.Squad) bool {
    if roleAllowed(member.Role, "owner", "admin") {
        return true
    }
    return member.UserID == squad.CreatorID
}

// requireSquadManager loads the squad and confirms the caller may
// manage it. On failure it writes the appropriate response and returns
// ok=false; callers must return immediately.
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

Rationale:

- `db.Member.UserID` and `db.Squad.CreatorID` are both `pgtype.UUID`;
  direct `==` works because the struct embeds a fixed-size byte array.
- The error string `"insufficient permissions"` is preserved verbatim so
  any existing client-side handling continues to work.

### 3.2 Endpoint changes

| Endpoint              | Current gate                                                 | New gate                                                |
| --------------------- | ------------------------------------------------------------ | ------------------------------------------------------- |
| `CreateSquad`           | `requireWorkspaceRole(..., "owner", "admin")` (line 119)     | `requireWorkspaceMember(..., "workspace not found")`    |
| `UpdateSquad`           | `requireWorkspaceRole + loadSquadInWorkspace` (lines 203, ~210) | `requireSquadManager`                                |
| `DeleteSquad`           | same (lines 278, ~285)                                       | `requireSquadManager`                                   |
| `AddSquadMember`        | same (lines 339, ~346)                                       | `requireSquadManager`                                   |
| `RemoveSquadMember`     | same (lines 415, ~422)                                       | `requireSquadManager`                                   |
| `UpdateSquadMemberRole` | same (lines 466, ~473)                                       | `requireSquadManager`                                   |

For the five "manager" endpoints, `requireSquadManager` returns the same
`(squad, member, ok)` triple that the original two-step pattern produced,
so the rest of each function body is unchanged.

For `CreateSquad`, the returned `member` is used as the `creator_id` of the
new Squad — unchanged.

### 3.3 Edge cases

- **Creator leaves the workspace.** Their `user_id` is no longer a row in
  `members` for that workspace. `requireWorkspaceMember` returns 404
  before the predicate runs, so they cannot act. Workspace owner/admin
  remain able to mutate or delete the Squad.
- **`creator_id` integrity.** `server/migrations/084_squad.up.sql:8`
  declares `creator_id UUID NOT NULL`. The predicate can safely compare
  without a null check.
- **Stale UUIDs at boundaries.** All UUIDs entering the manager helpers
  flow through `loadSquadInWorkspace` and `requireWorkspaceMember`,
  which already perform validation via `util.ParseUUID`. No new boundary
  parsing is introduced.

## 4. Frontend Design

### 4.1 Authorization predicate

In `packages/views/squads/components/squad-detail-page.tsx`, next to the
existing `isWorkspaceAdmin` selector (around line 96):

```ts
const canManageSquad =
  isWorkspaceAdmin || squad.creator_id === currentUser?.id;
```

`currentUser` is already read on line 91 via `useAuthStore`.

### 4.2 Control gating

The detail page wires callbacks down into `SquadDetailInspector`,
`SquadOverviewPane`, and an in-page archive button. Each child component
already follows the convention "if the callback prop is `undefined`, do
not render the corresponding control" (this is how `onCreateAgentClick`
is hidden for non-admins today). We extend the same pattern, so the
child components themselves do not change.

Pass-through changes in `squad-detail-page.tsx` (line references
approximate; final positions to be confirmed during implementation):

| Prop                                       | Gating expression                |
| ------------------------------------------ | -------------------------------- |
| `SquadDetailInspector.onRename`              | `canManageSquad` (else undefined) |
| `SquadDetailInspector.onUpdateDescription`   | `canManageSquad` (else undefined) |
| `SquadDetailInspector.onUploadAvatar`        | `canManageSquad` (else undefined) |
| `SquadOverviewPane.onAddMemberClick`         | `canManageSquad` (else undefined) |
| `SquadOverviewPane.onSetLeader`              | `canManageSquad` (else undefined) |
| `SquadOverviewPane.onRemoveMember`           | `canManageSquad` (else undefined) |
| `SquadOverviewPane.onUpdateRole`             | `canManageSquad` (else undefined) |
| `SquadOverviewPane.onSaveInstructions`       | `canManageSquad` (else undefined) |
| Archive / delete `Button` in `PageHeader` (around line 209-213) | render only if `canManageSquad` |
| `SquadOverviewPane.onCreateAgentClick`       | **unchanged**, stays `isWorkspaceAdmin`-gated |

`onCreateAgentClick` and `canManageSquad` are deliberately distinct
authorizations:

- `canManageSquad` answers "can the caller change *this* Squad's
  configuration."
- `isWorkspaceAdmin` answers "can the caller create a new Agent in this
  workspace." Creating an Agent is a workspace-level governance action
  and remains owner/admin-only regardless of Squad ownership.

### 4.3 Squad list page

`packages/views/squads/components/squads-page.tsx:78` already renders the
"Create Squad" button unconditionally. No change.

### 4.4 i18n and error UI

No new strings. The defensive `toast.error("Failed to ...")` paths
already exist on each mutation; with the new hiding logic they should be
unreachable in normal usage but stay as last-resort defense.

## 5. Data Model

No changes. `squads.creator_id` already exists, is `NOT NULL`, and is set
by `CreateSquad` to the authenticated caller's `user_id`. It is reused as
the authorization key.

## 6. Testing

### 6.1 Go tests (`server/internal/handler/squad_test.go`)

Add cases (extending the existing test fixtures and patterns):

- `CreateSquad` succeeds for a workspace `member`.
- `UpdateSquad` succeeds when the caller is the creator and a `member`.
- `UpdateSquad` returns 403 for a `member` who is not the creator.
- Same coverage for `DeleteSquad`.
- One representative test for each of `AddSquadMember`,
  `RemoveSquadMember`, `UpdateSquadMemberRole`:
  - creator-as-member → 2xx,
  - non-creator member → 403.
- Regression: `owner` and `admin` can still mutate Squads they did not
  create.

### 6.2 Frontend tests

Add `packages/views/squads/components/squad-detail-page.test.tsx`
(creating the file if absent; otherwise extend):

- With `currentUser.id === squad.creator_id` and role `member`, the
  rename, upload-avatar, add-member, set-leader, remove-member, and
  archive controls render.
- With role `member` and `currentUser.id !== squad.creator_id`, none of
  the above render.
- With role `admin` and a different creator, all controls render.
- "Create Agent within Squad" trigger stays governed by
  `isWorkspaceAdmin` and is unaffected by `canManageSquad`.

Mock `@multica/core/api` for mutation calls per the project's testing
conventions (see `CLAUDE.md` → Testing Rules). Do not mock `next/*` or
`react-router-dom`: this is a `packages/views` test.

### 6.3 E2E

No new Playwright tests. The combination of Go and view-level tests
above covers both the authorization decision and the UI propagation;
adding E2E here would not buy additional confidence.

## 7. Rollout and Compatibility

- No DB migration.
- No request / response schema change.
- Older desktop builds talking to the new server: still work; they will
  not surface the new permission to `member` users (the desktop UI may
  still hide some controls based on its own outdated assumptions), but
  nothing crashes.
- Newer desktop talking to an older server: a `member` clicking "Create
  Squad" still gets 403, identical to today's behavior. No regression.
- No feature flag.

## 8. Risks

- **Frontend prop-drilling drift.** The gating lives at the parent level
  while the rendering decision lives in children. If a future change
  passes the callback unconditionally, the gate silently breaks. The Go
  authorization remains the source of truth, so this is a UX-only
  failure mode (buttons that 403) rather than a security one. The
  frontend tests in §6.2 guard against this in CI.
- **Predicate divergence.** Backend and frontend each implement
  `canManageSquad` independently. They are simple enough (one boolean
  OR) that drift is unlikely, but if a third role ever joins the
  picture, both sides must be updated. No shared library is introduced
  to avoid premature abstraction.
