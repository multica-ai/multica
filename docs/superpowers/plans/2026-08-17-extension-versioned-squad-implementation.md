# Versioned Extension Squad Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to execute this plan task by task.

**Goal:** Turn the accepted Extensions-page prototype into the product flow: an Extension release is reviewed and configured before import, creates one independently versioned Squad, and materializes Agents and Skills that are usable only by that Squad.

**Architecture:** Keep an Extension release immutable by `(workspace_id, extension_key, version, digest)`. Add a non-persisting preview endpoint for a ZIP and a configured confirm-import request. A confirmed release creates a fixed-runtime, `kind = system` Agent per Extension Agent and a Skill carrying an internal-squad provenance marker. Public Agent and Skill handlers use public-only SQL queries; Squad execution continues to resolve the same rows internally through its member and agent-skill relations. Release resources remain serialized in `platform_extension_release.resources` so list/detail/update endpoints can render the versioned mapping without adding database foreign keys.

**Tech Stack:** Go + Chi, PostgreSQL + sqlc, React/TypeScript, TanStack Query, Zod, existing Multica UI components, Vitest, Go test.

## Product decisions locked by the approved prototype

- Exactly one Command ending in `-e2e` is required. It becomes the Squad instructions; every other Command becomes an internal generated Skill.
- The suffix before `-e2e` is the editable Squad base-name default. The visible final Squad name is always `base name · v<extension version>`; the suffix is read-only.
- A new Extension release always creates a separate versioned Squad. It never overwrites a previous version. Same key/version/digest is idempotent; same key/version with a different digest returns `EXTENSION_VERSION_IMMUTABLE`.
- Each Extension Agent is fixed to one manually selected, compatible workspace Runtime. The preview selects the current member's compatible `platform-agent-cli` Runtime by default, but the user may choose another runtime they are authorized to use. This flow never uses Runtime Pool allocation.
- Imported Agents and Skills are internal Squad resources. They are unavailable from global Agents/Skills lists and ordinary Agent/Skill mutation or task-assignment surfaces, but remain readable by Squad execution through `squad_member` and `agent_skill`.
- Archiving a version archives its Squad from ordinary Squad selection but preserves Extension history and all existing data. It does not mutate another release.

## Task 1: Establish versioned-import API contracts and persistence helpers

**Files:**
- Modify: `server/internal/handler/platform_extension.go`
- Modify: `server/internal/handler/platform_extension_contract.go`
- Modify: `server/pkg/db/queries/platform_extension.sql`
- Modify: `server/pkg/db/queries/agent.sql`
- Modify: `server/pkg/db/queries/skill.sql`
- Modify: `server/pkg/db/queries/squad.sql`
- Regenerate: `server/pkg/db/generated/platform_extension.sql.go`
- Regenerate: `server/pkg/db/generated/agent.sql.go`
- Regenerate: `server/pkg/db/generated/skill.sql.go`
- Regenerate: `server/pkg/db/generated/squad.sql.go`
- Test: `server/internal/handler/platform_extension_contract_test.go`
- Test: `server/internal/handler/platform_extension_import_test.go`

**Interfaces:**

```go
type PlatformExtensionImportConfig struct {
    SquadBaseName string                                `json:"squad_base_name"`
    AgentRuntimeIDs map[string]string                   `json:"agent_runtime_ids"`
}

type PlatformExtensionImportPreviewResponse struct {
    Release PlatformExtensionReleaseResponse            `json:"release"`
    SquadBaseName string                                `json:"squad_base_name"`
    Agents []PlatformExtensionPreviewAgentResponse      `json:"agents"`
    Runtimes []PlatformExtensionRuntimeResponse         `json:"runtimes"`
    Manifest json.RawMessage                            `json:"manifest"`
}
```

- `POST /api/extensions/preview` accepts the same raw ZIP/JSON document as import but writes nothing.
- `POST /api/extensions/import` accepts the package plus `PlatformExtensionImportConfig` in multipart form. Raw ZIP calls remain supported as the installed-client compatibility path and use server defaults.
- `PATCH /api/extensions/{id}` accepts the same configuration shape for a persisted release; `POST /api/extensions/{id}/archive` archives only that release's Squad.
- Agent mapping responses gain a `runtime` object. Keep the legacy top-level `runtime` projection populated from the leader's selected runtime so older clients still parse it.

Steps:

1. Add focused contract tests for `-e2e` validation: reject zero or multiple canonical flow Commands; accept exactly one and derive `delegate` from `delegate-e2e`.
2. Add import-preview tests that seed two compatible runtimes, verify the requester's runtime is selected first, verify no release/Agent/Skill/Squad row is written, and verify ZIP and JSON share the parser.
3. Add `PlatformExtensionImportConfig` parsing with strict duplicate/unknown-field rejection. Normalize the base name, reject empty names, require exactly one runtime ID per source Agent, and reject unknown source keys.
4. Add SQL helpers for release locking/resource replacement, fixed-runtime validation/locking, creation of internal `kind = 'system'` extension Agents, and public-only Skill lookup/listing. Keep all relationships application-managed; do not add a foreign key.
5. Run `make sqlc`, then run the focused handler contract and import tests. They must be RED before the next task modifies materialization.

## Task 2: Materialize independent versioned Squads with per-Agent fixed runtimes

**Files:**
- Modify: `server/internal/handler/platform_extension.go`
- Modify: `server/internal/handler/platform_extension_contract.go`
- Modify: `server/internal/handler/platform_extension_import_test.go`
- Modify: `server/internal/handler/platform_extension_allocator_test.go`
- Test: `server/internal/handler/platform_extension_visibility_test.go` (new)

**Implementation:**

1. Replace the current transaction-external idle-runtime selection with `previewPlatformExtensionImport`: load authorized compatible runtimes, order current-member-owned rows first, then deterministic authorization-compatible rows, and expose them to the editor. Never call Runtime Pool code.
2. On confirmed import, lock every selected Runtime in sorted UUID order, re-check workspace, `platform-agent-cli` provider, online/liveness, and `canUseRuntimeForAgent` while locked. Any invalid selection rolls back the full import.
3. Create source Skills and generated-command Skills with `config.origin = { type, release_id, source_key, scope: "squad_internal" }`. Keep SKILL.md/binary-file behavior unchanged.
4. Create each Extension Agent through the dedicated internal-agent query with `kind = 'system'`, a stable `system_key` containing release ID and source key, `runtime_binding_mode = 'fixed'`, and that Agent's selected Runtime. Bind every internal Skill exactly as today.
5. Create the Squad as `"<base> · v<version>"`, set its instructions from the sole `-e2e` Command, and add all internal Agents as Squad members. Serialize the base name, Squad, Agent-to-runtime mappings, and Skills into release resources.
6. Preserve idempotency by returning the stored mapping for the same digest. Preserve the version-immutable conflict for a different digest. Never update a prior release while importing a later version.
7. Add database-backed tests for two releases of one extension, independently named/versioned Squads, distinct Agent runtimes, rollback on one invalid runtime, same-version idempotency, and changed same-version rejection.
8. Run `go test ./internal/handler -run 'Test(ImportPlatformExtension|PlatformExtension)' -count=1` and `make sqlc`; regenerate code only from the pinned sqlc version and check idempotence.

## Task 3: Enforce internal-resource visibility and direct-assignment boundaries

**Files:**
- Modify: `server/pkg/db/queries/skill.sql`
- Modify: `server/internal/handler/skill.go`
- Modify: `server/internal/handler/agent.go`
- Modify: `server/internal/handler/issue.go`
- Modify: `server/internal/handler/quick_action.go` (only if its Agent search bypasses `ListAgents`)
- Test: `server/internal/handler/platform_extension_visibility_test.go`
- Test: `server/internal/handler/skill_list_test.go`
- Test: `server/internal/handler/agent_test.go`
- Test: `server/internal/handler/issue_agent_create_e2e_test.go`

**Rules:**

- Global Agent queries stay `kind = 'user'`; imported extension Agents use `kind = 'system'` and therefore cannot be loaded by `GetAgentInWorkspace`, normal Agent pickers, direct Agent issue assignment, mentions, or normal Squad-member editing.
- Add public-only Skill queries that exclude `config #>> '{origin,scope}' = 'squad_internal'`. Use those queries in List/Get/attach/remove/set public Skill handlers. Internal import and internal Squad execution continue to use unrestricted relation queries.
- A Squad route may resolve its system leader via its existing internal `GetAgent` path. Do not change Squad execution, briefing, or agent-skill loading to public-only queries.

Steps:

1. Add a failing test that imports an extension then verifies its Agents are absent from `GET /api/agents`, its Skills absent from `GET /api/skills`, and direct `agent_id`, Agent-Skill attach, Get/Update/Delete Skill requests are rejected/not found.
2. Add a failing positive test that assigns the imported Squad and verifies the hidden leader and hidden Skills are still resolved in its task/briefing path.
3. Implement public-only Skill SQL and handler loaders, preserving unrestricted internal queries for import, `ListAgentSkills`, and Squad briefing.
4. Audit ordinary task/mention/quick-action Agent entry points. Use their existing `GetAgentInWorkspace kind='user'` gate or add the same explicit guard where a raw `GetAgent` is used.
5. Run the focused visibility, Agent list, Skill list, direct-assignment, and Squad execution tests; verify no global list or direct endpoint leaks an internal resource.

## Task 4: Persist mapping edits and archive behavior

**Files:**
- Modify: `server/internal/handler/platform_extension.go`
- Modify: `server/pkg/db/queries/platform_extension.sql`
- Modify: `server/pkg/db/queries/squad.sql`
- Regenerate: `server/pkg/db/generated/platform_extension.sql.go`
- Regenerate: `server/pkg/db/generated/squad.sql.go`
- Test: `server/internal/handler/platform_extension_import_test.go`
- Test: `server/internal/handler/platform_extension_archive_test.go`

**Implementation:**

1. Add a release update transaction: lock the release, lock its Squad, lock exactly the stored internal Agent IDs, re-validate their extension provenance, and validate replacement runtimes before writing. Update `squad.name` only as `base + " · v" + release.version`; update internal Agent runtime IDs; replace the serialized mapping; commit atomically.
2. Add an archive transaction that verifies the release mapping and calls `ArchiveSquad`. Return the release with `squad.archived = true`; retain it in `GET /api/extensions` history while ordinary Squad selection excludes it through the existing `ListSquads` archived filter.
3. Add tests for name-only update, one-Agent runtime update, concurrent/non-extension resource rejection, failed update rollback, archive preserving release detail, and a later version leaving an archived earlier release untouched.
4. Run the focused extension import/archive suite plus `go vet ./internal/handler ./pkg/db/generated`.

## Task 5: Expose preview/configuration and release mutation in the shared API client

**Files:**
- Modify: `packages/core/extensions/types.ts`
- Modify: `packages/core/extensions/schemas.ts`
- Modify: `packages/core/extensions/api-client.test.ts`
- Modify: `packages/core/extensions/schemas.test.ts`
- Modify: `packages/core/extensions/mutations.ts`
- Modify: `packages/core/extensions/queries.ts`
- Modify: `packages/core/api/client.ts`
- Test: `packages/core/extensions/mutations.test.tsx`

**Interfaces:**

```ts
export interface PlatformExtensionImportConfig {
  squad_base_name: string;
  agent_runtime_ids: Record<string, string>;
}

export interface PlatformExtensionImportPreview { /* release, manifest, default config, choices */ }

api.previewPlatformExtension(document: Uint8Array): Promise<PlatformExtensionImportPreview | null>
api.importPlatformExtension(document: Uint8Array, config: PlatformExtensionImportConfig): Promise<PlatformExtensionImportResult | null>
api.updatePlatformExtension(id: string, config: PlatformExtensionImportConfig): Promise<PlatformExtensionMapping | null>
api.archivePlatformExtension(id: string): Promise<PlatformExtensionMapping | null>
```

Steps:

1. Extend Zod models with agent-level runtime, editable `squad_base_name`, `archived`, preview runtime choices, and import configuration. Leave legacy top-level `runtime` optional and defensively ignored by the new view.
2. Add API-client tests for raw ZIP preview, multipart configured import, PATCH update, archive, bad response fallback, and the 16 MiB client-side size boundary.
3. Update mutation invalidation: preview is local/ephemeral; confirmed import/update/archive invalidate extension history/detail plus Agents, Skills, Squads, and Runtime projections after server success.
4. Run `pnpm --filter @multica/core test -- extensions` and `pnpm --filter @multica/core typecheck`.

## Task 6: Implement the accepted Extensions page

**Files:**
- Modify: `packages/views/extensions/extensions-page.tsx`
- Modify: `packages/views/extensions/extensions-page.test.tsx`
- Modify: `packages/views/locales/en/extensions.json`
- Modify: `packages/views/locales/zh-Hans/extensions.json`
- Modify: `packages/views/locales/ja/extensions.json`
- Modify: `packages/views/locales/ko/extensions.json`

**Interaction contract:**

1. Keep the standard Multica page shell and the right-top `导入 Extension` button. It opens only a compact ZIP picker dialog.
2. On valid ZIP selection, call preview and show the pending release in the same selected-detail panel; do not create resources until `确认导入`.
3. Keep left-side Extension/version history. The selected version opens the same three tabs: `原子能力映射`, `资源清单`, `导入信息`.
4. In `原子能力映射`, show vertical source-to-target rows with one arrow, a leading check, one editable Squad base-name input followed by a read-only `· vX.Y.Z` suffix, and one editable Runtime selector on each Agent row. Use existing runtime query data; options must indicate the current user's default but still respect server validation.
5. Green checks represent persisted rows. Unsaved Squad-name or Agent-runtime edits make only their own checks neutral. Pending import begins neutral. Confirm/save restores checks after successful API response and shows the compact in-place step result; no second configuration/pipeline screen.
6. In `资源清单`, show the versioned Squad and grouped internal resources. Each Agent row includes its bound Runtime. Display exactly this scope copy: `下列 Agent 与 Skills 都是该小队的内部资源，仅在小队编排与执行时生效，不会出现在全局“智能体”或“Skills”列表，也不能作为普通任务的直接分配对象。` Do not link internal Agents/Skills to global detail pages.
7. In `导入信息`, show release digest/version/immutable-version status and archive action for persisted non-archived versions. Archived entries remain visible in history.

Steps:

1. Replace the existing immediate file-change mutation test with a preview-first RED test. Add tests for compact picker, versioned name suffix immutability, per-agent runtime selection, check-state transitions, confirmation request, persisted-edit save, archive, resource scope copy, and no internal-resource global links.
2. Implement the single-page state machine using React Query for persisted data and component state only for the selected preview/config edits.
3. Use only semantic design tokens and existing `Card`, `Badge`, `Dialog`, `Select`, and `Alert` primitives. Keep history/detail responsive without a separate status-pane/wizard.
4. Add every new string in all four locale files following the repository naming conventions.
5. Run the focused page suite, then `pnpm --filter @multica/views typecheck`.

## Task 7: Regression verification and manual client handoff

**Files:**
- Modify only if a verification failure requires its owning task's listed file.

Steps:

1. Run `make sqlc`, rerun it, and confirm generated query files are unchanged on the second run.
2. Run focused Go tests for extension contract/import/archive/visibility and public Agent/Skill surfaces. Run `go vet ./internal/handler ./pkg/db/generated`.
3. Run focused core and views extension tests and package typechecks.
4. Run `git diff --check`; do not stage or commit because the worktree already contains unrelated user changes.
5. Hand off manual verification steps: import `testdata/extensions/runtime-pool-demo.zip`, select every Agent's fixed runtime, confirm `delegate · v<version>`, verify hidden Agents/Skills do not appear in global lists, assign the Squad, then import a bumped version and archive the older version.
