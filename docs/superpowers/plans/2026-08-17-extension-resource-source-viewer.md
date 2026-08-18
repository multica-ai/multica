# Extension Resource Source Viewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users inspect imported internal Agent and Skill source files from the Extensions resource inventory without mutating an Extension release.

**Architecture:** Reuse the canonical Bundle already returned as `PlatformExtensionDetail.manifest`. Core types preserve Agent prompts and Skill file entries, then the Extensions page derives a read-only virtual file tree. A wide dialog presents the tree and selected file; binary base64 entries never enter the text renderer.

**Tech Stack:** React, TypeScript, TanStack Query, Radix/Base UI dialog, Vitest, existing Extension APIs.

## Global Constraints

- Use `agents/<agent-key>.md` for a virtual Agent source file.
- Use `skills/<skill-key>/SKILL.md` and nested declared Skill paths verbatim.
- The viewer is read-only and uses no write endpoint.
- Binary entries are identified only by `encoding: "base64"`; display their decoded byte size.
- Preserve current ZIP import, release history, runtime selection, and mapping behavior.

---

### Task 1: Preserve source contents in core Extension types

**Files:**
- Modify: `packages/core/extensions/types.ts`
- Modify: `packages/core/extensions/schemas.ts`
- Modify: `packages/core/extensions/schemas.test.ts`

**Interfaces:**
- Produces `PlatformExtensionManifestAgent.prompt?: string` and strict manifest Skill files `{ path, content?, encoding? }` for the Extension viewer.
- Consumes the existing detail endpoint `manifest` object without an API contract change.

- [ ] **Step 1: Write the failing manifest parsing test**

```ts
const parsed = PlatformExtensionDetailSchema.parse({
  ...mapping,
  manifest: {
    agents: [{ key: "lead", name: "Lead", prompt: "# Lead" }],
    skills: [{ key: "evidence", name: "Evidence", files: [
      { path: "SKILL.md", content: "# Evidence" },
      { path: "assets/logo.bin", content: "AP8=", encoding: "base64" },
    ] }],
  },
});
expect(parsed.manifest.agents?.[0]?.prompt).toBe("# Lead");
expect(parsed.manifest.skills?.[0]?.files?.[1]?.encoding).toBe("base64");
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `pnpm --filter @multica/core exec vitest run extensions/schemas.test.ts`

Expected: the typed manifest lacks the source fields or drops them.

- [ ] **Step 3: Add minimal typed manifest fields**

```ts
export interface PlatformExtensionManifestAgent {
  key?: string;
  name: string;
  description?: string;
  prompt?: string;
}

export interface PlatformExtensionManifestSkillFile {
  path: string;
  content?: string;
  encoding?: "base64";
}
```

Mirror those optional fields in the recursive Zod manifest schema so normal old manifests remain valid.

- [ ] **Step 4: Run the focused test and core typecheck**

Run: `pnpm --filter @multica/core exec vitest run extensions/schemas.test.ts && pnpm --filter @multica/core typecheck`

Expected: PASS.

### Task 2: Derive a safe read-only virtual file tree

**Files:**
- Modify: `packages/views/extensions/extensions-page.tsx`
- Modify: `packages/views/extensions/extensions-page.test.tsx`

**Interfaces:**
- Produces local `ExtensionSourceFile { id, path, content, binary, byteSize }` and `ExtensionSourceResource { type, name, files }` values.
- Consumes `PlatformExtensionManifest` from Task 1.

- [ ] **Step 1: Write failing viewer derivation tests through the page**

```tsx
fireEvent.click(screen.getByRole("tab", { name: "Resource inventory" }));
fireEvent.click(screen.getByRole("button", { name: "Delegate Lead" }));
expect(await screen.findByText("agents/lead.md")).toBeInTheDocument();
expect(screen.getByText("# Lead")).toBeInTheDocument();
```

Add a Skill case selecting `references/checklist.md`, and a base64 asset case asserting `Binary file` plus its decoded byte count rather than its base64 string.

- [ ] **Step 2: Run the focused page test and verify it fails**

Run: `pnpm --filter @multica/views exec vitest run extensions/extensions-page.test.tsx`

Expected: resource rows are not interactive and no source viewer is rendered.

- [ ] **Step 3: Add pure derivation helpers in the page module**

```ts
function agentSourceResource(agent: PlatformExtensionManifestAgent): ExtensionSourceResource {
  return { type: "agent", name: agent.name, files: [{
    id: `agent:${agent.key}`, path: `agents/${agent.key}.md`,
    content: agent.prompt ?? "", binary: false, byteSize: 0,
  }] };
}
```

For each Skill file, prefix `skills/<key>/`, retain its path, mark `encoding === "base64"` as binary, and calculate decoded bytes with guarded base64 decoding. Use an empty fallback resource only when the canonical manifest truly omits content.

- [ ] **Step 4: Make resource rows clickable without changing mapping controls**

Replace Agent cards and Skill chips in `ResourceInventory` with buttons that set the selected source resource. Keep the existing Agent runtime pill and internal-resource explanation.

- [ ] **Step 5: Run focused page test**

Run: `pnpm --filter @multica/views exec vitest run extensions/extensions-page.test.tsx`

Expected: PASS.

### Task 3: Render the wide source dialog

**Files:**
- Modify: `packages/views/extensions/extensions-page.tsx`
- Modify: `packages/views/extensions/extensions-page.test.tsx`

**Interfaces:**
- Consumes `ExtensionSourceResource` from Task 2.
- Produces a closeable, read-only dialog with a selectable file tree.

- [ ] **Step 1: Write the failing interaction tests**

```tsx
expect(screen.getByRole("dialog")).toHaveClass("sm:max-w-5xl");
fireEvent.click(screen.getByRole("button", { name: "references/checklist.md" }));
expect(screen.getByText("Cite claims next to evidence.")).toBeInTheDocument();
expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
```

- [ ] **Step 2: Implement `ExtensionResourceSourceDialog`**

Use the existing `Dialog` primitives with `sm:max-w-5xl`. Render a left `nav` file tree from sorted file paths and a right `pre` code area. Initialize selection to the sole Agent file or the Skill `SKILL.md`, then the first file if `SKILL.md` is absent. Render a binary state with path and decoded byte size; never render `content` in a `pre` for binary files.

- [ ] **Step 3: Verify close and selection reset behavior**

Close must clear the selected resource. Reopening another Agent or Skill must start on that resource’s default file, not the prior selected file.

- [ ] **Step 4: Run focused tests and static checks**

Run: `pnpm --filter @multica/views exec vitest run extensions/extensions-page.test.tsx && pnpm --filter @multica/core typecheck && git diff --check`

Expected: PASS. If whole Views typecheck reports existing unrelated module/type errors, record them separately and ensure it reports no error in `extensions-page.tsx`.
