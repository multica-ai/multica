# Extension Import Center Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the plain Extension release cards with a client-verifiable import center showing import history, selected Extension details, and atomic mappings.

**Architecture:** Keep the existing list/import/detail API and import mutation unchanged. Derive the visual atomic mapping from the canonical detail manifest when available, with a safe mapping-only fallback for freshly imported data. Use one shared `-e2e` suffix constant in the core extension package so the UI can identify Flow Commands without coupling to backend internals.

**Tech Stack:** React, TypeScript, TanStack Query, existing Multica UI Card/Badge/Alert components, Vitest + Testing Library.

## Global Constraints

- The page must keep existing JSON import validation, idempotent import behavior, runtime-unavailable errors, and resource links.
- A Flow Command is identified by the configurable `-e2e` suffix; the prototype displays it as `delegate-e2e`.
- Non-`-e2e` Commands are displayed as Generated Skills; no Runtime Pool allocator behavior changes are included.
- Mapping rows contain only one relationship arrow: source primitive → generated Multica resource.
- Existing `.flow` server compatibility is not removed; `-e2e` is additionally accepted as the canonical Flow marker.

---

### Task 1: Add shared Flow Command marker and manifest view types

**Files:**
- Modify: `packages/core/extensions/types.ts`
- Test: `packages/core/extensions/types.test.ts`
- Modify: `server/internal/handler/platform_extension_contract.go`
- Test: `server/internal/handler/platform_extension_contract_test.go`

**Interfaces:**
- Produce `PLATFORM_EXTENSION_FLOW_COMMAND_SUFFIX = "-e2e"` in the client and `PlatformExtensionFlowCommandSuffix = "-e2e"` in the server contract.
- Produce `PlatformExtensionManifest`, `PlatformExtensionManifestCommand`, and `PlatformExtensionManifestSkill` types used by the page.

- [ ] **Step 1: Write the failing type/helper test** asserting the exported suffix is `-e2e` and a manifest command ending in `-e2e` is classified as Flow while `summarize` is not.
- [ ] **Step 2: Run the focused core test** and verify it fails because the shared marker/helper is missing.
- [ ] **Step 3: Add the exported constant, manifest types, and a pure `classifyPlatformExtensionCommand` helper.** The helper must use `name.endsWith(PLATFORM_EXTENSION_FLOW_COMMAND_SUFFIX)`.
- [ ] **Step 4: Run the focused core test** and verify it passes.

### Task 2: Write the Extensions page behavior tests

**Files:**
- Modify: `packages/views/extensions/extensions-page.test.tsx`

**Interfaces:**
- Consume the existing mocked `PlatformExtensionMapping` and `PlatformExtensionDetail` data.
- Cover visible labels and selected history behavior without changing API mocks.

- [ ] **Step 1: Add a test for the three-region layout** asserting `Import history`, `Extension details`, and `Current import status` are visible.
- [ ] **Step 2: Add a test for atomic mappings** using a detail manifest with `delegate-e2e`, `evidence`, and `summarize`, asserting the Flow Command maps to `Squad Instructions` and the other two map to `Generated Skill`.
- [ ] **Step 3: Add a test for the selected history arrow** asserting the active release row has an accessible selected state and the detail heading follows the selected release.
- [ ] **Step 4: Run the focused page tests** and verify the new tests fail against the old two-card layout.

### Task 3: Implement the redesigned import center

**Files:**
- Modify: `packages/views/extensions/extensions-page.tsx`
- Modify: `packages/views/locales/en/extensions.json`
- Modify: `packages/views/locales/zh-Hans/extensions.json`

**Interfaces:**
- Consume `PlatformExtensionManifest` and `classifyPlatformExtensionCommand` from `@multica/core/extensions`.
- Preserve `useImportPlatformExtension`, `extensionListOptions`, and `extensionDetailOptions` behavior.

- [ ] **Step 1: Replace the release-only left card with an import history list** showing extension key, version, resource counts, current status, and `aria-pressed` selection.
- [ ] **Step 2: Add the selected Extension details header** with digest, version, and one relationship arrow per atomic mapping row.
- [ ] **Step 3: Add the mapping rows:** `-e2e` Command → Squad Instructions, all other Commands → Generated Skill, Agent → Agent, source Skill → Skill.
- [ ] **Step 4: Add the current import status card** showing release completion, Runtime readiness, Agent/Skill counts, and the next manual Pool verification action.
- [ ] **Step 5: Keep the existing import input, success/error alerts, loading states, empty state, and resource links intact.**
- [ ] **Step 6: Add responsive classes so the three regions collapse to one column on narrow screens.**
- [ ] **Step 7: Run focused page tests** and verify the new behavior passes.

### Task 4: Verify the prototype package

**Files:**
- No new production files.

- [ ] **Step 1: Run the focused Extensions page test suite.**
- [ ] **Step 2: Run the core extension type/helper tests.**
- [ ] **Step 3: Run `git diff --check` for the changed files.**
- [ ] **Step 4: Run `pnpm --filter @multica/core typecheck` and `pnpm --filter @multica/views typecheck`.**
- [ ] **Step 5: Report the exact page route and manual verification steps to the user.
