# Extension 受管小队 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mark Extension-created Squads as managed, protect their internal composition, and expose their internal Agent details only from the Squad page.

**Architecture:** `platform_extension_release.squad_id` remains the sole ownership source. The Squad API enriches list/detail payloads with immutable Extension ownership metadata and exposes a Squad-scoped internal-Agent read endpoint. The UI uses that metadata to render the badge, remove composition controls, and open a read-only inspector instead of navigating to the global Agent route.

**Tech Stack:** Go/chi/pgx/sqlc, TypeScript/Zod/React Query, React, Vitest.

## Global Constraints

- Determine management only from `platform_extension_release.squad_id`; never infer it from a name or `system_key`.
- Do not alter normal Squad behavior or global Agent list visibility.
- Extension internal Agent details are read-only and only available through the owning Squad.
- Public mutation endpoints must reject managed Squad membership and leader changes.
- Use test-first red/green cycles; regenerate sqlc after query changes.

---

### Task 1: Server-owned managed-Squad metadata and protection

**Files:**
- Modify: `server/pkg/db/queries/platform_extension.sql`
- Modify: `server/internal/handler/squad.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/pkg/db/generated/platform_extension.sql.go`
- Test: `server/internal/handler/squad_test.go`

**Interfaces:**
- Produces `SquadResponse.extension` as `{ release_id, extension_key, version } | null`.
- Produces `GET /api/squads/{squadId}/internal-agents/{agentId}`.
- Rejects managed Squad membership / leader writes with HTTP 409 and `EXTENSION_MANAGED_SQUAD`.

- [ ] **Step 1: Write failing handler tests**

```go
func TestManagedExtensionSquadIncludesExtensionIdentity(t *testing.T) {
    // Seed a Squad plus platform_extension_release.squad_id.
    // GET list and detail must include extension.key/version/release_id.
}

func TestManagedExtensionSquadRejectsCompositionWrites(t *testing.T) {
    // POST member, DELETE member, PATCH member role, and PUT leader_id return
    // 409 with code EXTENSION_MANAGED_SQUAD.
}

func TestManagedExtensionSquadReadsInternalAgentOnlyInItsSquad(t *testing.T) {
    // GET scoped internal Agent returns the internal Agent detail; another
    // Squad or an ordinary Agent returns 404.
}
```

- [ ] **Step 2: Run the tests to verify RED**

Run: `source scripts/project-go-env.sh && cd server && go test ./internal/handler -run 'TestManagedExtensionSquad' -count=1`

Expected: FAIL because the response field, scoped endpoint, and write guard do not exist.

- [ ] **Step 3: Add the canonical queries and handlers**

```sql
-- name: ListPlatformExtensionSquadBindings :many
SELECT id AS release_id, squad_id, extension_key, version
FROM platform_extension_release
WHERE workspace_id = $1 AND squad_id IS NOT NULL;
```

Build a `map[squadID]extensionIdentity` once for list/detail responses. Add
`ensureUnmanagedSquadForCompositionWrite` immediately after loading a squad
in member/leader mutation handlers. The scoped read endpoint must validate the
release binding, squad membership, `agent.kind = 'system'`, workspace match,
and then serialize only `id`, `name`, `description`, `instructions`, leader
role, runtime identity and Skills.

- [ ] **Step 4: Regenerate sqlc and wire the route**

Run: `cd server && sqlc generate`

Add `GET /api/squads/{squadId}/internal-agents/{agentId}` beside existing
Squad member routes.

- [ ] **Step 5: Run server tests to verify GREEN**

Run: `source scripts/project-go-env.sh && cd server && go test ./internal/handler -run 'TestManagedExtensionSquad' -count=1`

Expected: PASS.

### Task 2: Core contracts and client API

**Files:**
- Modify: `packages/core/types/squad.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/client.ts`
- Test: `packages/core/api/schemas.test.ts`

**Interfaces:**
- Consumes `SquadResponse.extension` and the scoped internal-Agent endpoint from Task 1.
- Produces `Squad.extension?: ExtensionManagedSquad | null` and
  `api.getSquadInternalAgent(squadId, agentId): Promise<SquadInternalAgent>`.

- [ ] **Step 1: Write failing schema/client tests**

```ts
it("parses Extension ownership on a Squad", () => {
  expect(SquadSchema.parse({ ...baseSquad, extension: {
    release_id: "release-1", extension_key: "delegate", version: "1.0.0",
  }}).extension?.extension_key).toBe("delegate");
});
```

- [ ] **Step 2: Run to verify RED**

Run: `pnpm --filter @multica/core exec vitest run api/schemas.test.ts`

Expected: FAIL because `extension` is unknown/omitted.

- [ ] **Step 3: Implement minimal types, schemas, and client method**

Add explicit `ExtensionManagedSquad` and `SquadInternalAgent` types. Extend
the lenient Squad schema with an optional nullable `extension` object. Parse
the scoped API result with a dedicated Zod schema instead of treating it as a
normal Agent response.

- [ ] **Step 4: Run to verify GREEN**

Run: `pnpm --filter @multica/core exec vitest run api/schemas.test.ts`

Expected: PASS.

### Task 3: List badge, locked composition, and in-page Agent inspector

**Files:**
- Modify: `packages/views/squads/components/squads-page.tsx`
- Modify: `packages/views/squads/components/squad-detail-page.tsx`
- Modify: `packages/views/locales/en/squads.json`
- Modify: `packages/views/locales/zh-Hans/squads.json`
- Test: `packages/views/squads/components/squads-page.test.tsx`
- Test: `packages/views/squads/components/squad-detail-page.test.tsx`

**Interfaces:**
- Consumes `Squad.extension` and `api.getSquadInternalAgent` from Task 2.
- Produces an `Extension` badge and a read-only `Sheet` inspector in Squad detail.

- [ ] **Step 1: Write failing view tests**

```tsx
it("marks Extension-managed Squads in the list", () => {
  render(<SquadsPage />);
  expect(screen.getByText("Extension")).toBeInTheDocument();
});

it("opens a read-only internal Agent inspector instead of an Agent link", async () => {
  render(<SquadDetailPage />);
  await user.click(screen.getByRole("button", { name: "View internal Agent" }));
  expect(await screen.findByRole("dialog", { name: "Pool Researcher" })).toBeInTheDocument();
  expect(screen.queryByLabelText("View agent details")).not.toBeInTheDocument();
});

it("does not show composition controls for an Extension-managed Squad", () => {
  render(<SquadDetailPage />);
  expect(screen.queryByText("Create Agent")).not.toBeInTheDocument();
  expect(screen.queryByText("Add member")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run to verify RED**

Run: `pnpm --filter @multica/views exec vitest run squads/components/squads-page.test.tsx squads/components/squad-detail-page.test.tsx`

Expected: FAIL because managed-Squad presentation and the read-only inspector do not exist.

- [ ] **Step 3: Implement isolated managed-Squad presentation**

Render a compact `Extension` badge in the list identity cell and detail header.
Derive `isManagedExtensionSquad = Boolean(squad.extension)`. Pass it through
the detail member pane to suppress creation and composition actions. Replace
the Agent link with a button only for managed internal Agents; its click loads
the scoped endpoint and opens a right-side `Sheet`. The inspector is display
only and renders the requested fields. Keep the existing link and all controls
for ordinary Squads.

- [ ] **Step 4: Run to verify GREEN**

Run: `pnpm --filter @multica/views exec vitest run squads/components/squads-page.test.tsx squads/components/squad-detail-page.test.tsx`

Expected: PASS.

### Task 4: Focused verification

**Files:**
- Verify only the files above.

- [ ] **Step 1: Run focused backend and frontend suites**

Run:
```bash
source scripts/project-go-env.sh && cd server && go test ./internal/handler -run 'TestManagedExtensionSquad' -count=1
pnpm --filter @multica/core exec vitest run api/schemas.test.ts
pnpm --filter @multica/views exec vitest run squads/components/squads-page.test.tsx squads/components/squad-detail-page.test.tsx
git diff --check
```

Expected: all commands pass.

- [ ] **Step 2: Commit the focused implementation**

```bash
git add server/pkg/db/queries/platform_extension.sql server/pkg/db/generated/platform_extension.sql.go \
  server/internal/handler/squad.go server/cmd/server/router.go \
  packages/core/types/squad.ts packages/core/api/schemas.ts packages/core/api/client.ts \
  packages/views/squads packages/views/locales/en/squads.json packages/views/locales/zh-Hans/squads.json
git commit -m "feat: protect extension managed squads"
```
