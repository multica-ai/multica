# Bulk Skill Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a workspace import many skills at once — from a local folder (Phase 1) and from a GitHub repo/folder URL (Phase 2) — through one shared discover→checklist→import→summary panel.

**Architecture:** Both sources produce a list of skill *candidates*; a shared concurrency engine imports the selected ones via the EXISTING single-skill endpoints (`POST /api/skills` for folder payloads, `POST /api/skills/import` for GitHub URLs). The only new backend surface is one read-only GitHub discovery endpoint. The UI mirrors the existing `runtime-local-skill-import-panel.tsx`.

**Tech Stack:** Go (chi handlers), React + TanStack Query, Vitest (TS), `go test` (Go). Skills + files live in Postgres TEXT (no S3/disk).

**Spec:** `docs/superpowers/specs/2026-06-01-bulk-skill-import-design.md`

---

## File Structure

**Phase 1 (folder import — frontend only):**
- Create `packages/views/skills/lib/frontmatter.ts` — parse SKILL.md YAML frontmatter (name/description). Pure, tested.
- Create `packages/views/skills/lib/folder-discovery.ts` — turn a browser `File[]` (with `webkitRelativePath`) into skill candidates with grouped files + caps/binary filtering. Pure, tested.
- Create `packages/views/skills/hooks/use-bulk-skill-import.ts` — the shared concurrency engine (10-wide pool, partial-success results, query invalidation). Tested.
- Create `packages/views/skills/components/bulk-skill-import-panel.tsx` — the panel: source toggle (Folder | GitHub), candidate checklist, progress, summary.
- Modify `packages/views/skills/components/create-skill-dialog.tsx` — add the `bulk` method card + wide dialog.
- Modify `packages/views/locales/{en,zh-Hans,ko,ru}/skills.json` — new i18n keys (parity-enforced).

**Phase 2 (GitHub discovery — backend + wiring):**
- Create `server/internal/handler/skill_discover.go` — `discoverGitHubSkills()` + `DiscoverSkills` handler.
- Create `server/internal/handler/skill_discover_test.go` — handler/unit tests.
- Modify `server/cmd/server/router.go` — register `POST /api/skills/discover`.
- Modify `packages/core/api/client.ts` — `discoverSkills()` method.
- Modify `packages/core/types/agent.ts` (or the skills types file) — `SkillCandidate`, `SkillDiscoveryResult`.
- Modify `packages/views/skills/components/bulk-skill-import-panel.tsx` — wire the GitHub source.

---

# PHASE 1 — Local folder import (ships independently)

## Task 1: i18n keys for bulk import

**Files:**
- Modify: `packages/views/locales/en/skills.json`
- Modify: `packages/views/locales/zh-Hans/skills.json`
- Modify: `packages/views/locales/ko/skills.json`
- Modify: `packages/views/locales/ru/skills.json`
- Test: `packages/views/locales/parity.test.ts` (existing — run, don't edit)

The panel reuses existing generic `runtime_import.*` keys (`bulk_progress`, `bulk_summary_imported/skipped/failed`, `select_all`, `bulk_done_button`, `bulk_cancel_button`, `bulk_complete_hint`, `bulk_cancelled_hint`). Only genuinely-new strings are added.

- [ ] **Step 1: Add the `bulk` method card + `bulk_import` group to EN**

In `en/skills.json`, add `bulk` under `create.method` and `bulk_title`/`bulk_desc` under `create.method_card`:

```jsonc
// inside "create": { "method": { ... } }
"bulk": {
  "title": "Import a set",
  "desc": "Import many skills at once from a GitHub repo or a local folder."
}
// inside "create": { "method_card": { ... } }
"bulk_title": "Import a set",
"bulk_desc": "Many skills at once — a GitHub repo or a local folder."
```

Add a new top-level `bulk_import` group to `en/skills.json`:

```jsonc
"bulk_import": {
  "source_label": "Source",
  "source_github": "GitHub repo",
  "source_folder": "Local folder",
  "github_url_label": "Repository or folder URL",
  "github_url_placeholder": "github.com/owner/repo or .../tree/main/skills",
  "discover": "Discover",
  "discovering": "Discovering…",
  "folder_pick": "Choose a folder",
  "folder_drop_hint": "or drop a folder here",
  "folder_reading": "Reading folder…",
  "candidates_title": "Found skills",
  "already_exists": "already exists",
  "empty_no_skills": "No SKILL.md found here.",
  "empty_hint": "Each skill is a folder containing a SKILL.md.",
  "capped_notice": "Showing the first {{count}} skills; more were found.",
  "folder_too_large": "Folder is too large to read in the browser.",
  "discover_failed": "Could not read that source.",
  "import_selected": "Import {{count}} selected"
}
```

- [ ] **Step 2: Mirror the same keys into zh-Hans / ko / ru**

Add the identical key paths to the other three locales. Per the glossary, `skill` / `SKILL.md` / `GitHub` stay English. Concrete values:

`ru/skills.json`:
```jsonc
"bulk": { "title": "Импорт набора", "desc": "Импорт сразу нескольких навыков из GitHub-репо или локальной папки." }
// method_card:
"bulk_title": "Импорт набора",
"bulk_desc": "Сразу несколько навыков — GitHub-репо или локальная папка.",
// bulk_import:
"bulk_import": {
  "source_label": "Источник",
  "source_github": "GitHub-репо",
  "source_folder": "Локальная папка",
  "github_url_label": "URL репозитория или папки",
  "github_url_placeholder": "github.com/owner/repo или .../tree/main/skills",
  "discover": "Найти",
  "discovering": "Поиск…",
  "folder_pick": "Выбрать папку",
  "folder_drop_hint": "или перетащите папку сюда",
  "folder_reading": "Чтение папки…",
  "candidates_title": "Найденные навыки",
  "already_exists": "уже есть",
  "empty_no_skills": "SKILL.md не найдены.",
  "empty_hint": "Каждый навык — это папка с файлом SKILL.md.",
  "capped_notice": "Показаны первые {{count}}; найдено больше.",
  "folder_too_large": "Папка слишком большая для чтения в браузере.",
  "discover_failed": "Не удалось прочитать источник.",
  "import_selected": "Импортировать выбранные ({{count}})"
}
```

`zh-Hans/skills.json` (keep `skill`/`GitHub` English):
```jsonc
"bulk": { "title": "批量导入", "desc": "从 GitHub 仓库或本地文件夹一次性导入多个 skill。" }
"bulk_title": "批量导入",
"bulk_desc": "一次导入多个 skill —— GitHub 仓库或本地文件夹。",
"bulk_import": {
  "source_label": "来源", "source_github": "GitHub 仓库", "source_folder": "本地文件夹",
  "github_url_label": "仓库或文件夹 URL",
  "github_url_placeholder": "github.com/owner/repo 或 .../tree/main/skills",
  "discover": "查找", "discovering": "查找中…",
  "folder_pick": "选择文件夹", "folder_drop_hint": "或把文件夹拖到这里",
  "folder_reading": "正在读取文件夹…",
  "candidates_title": "找到的 skill", "already_exists": "已存在",
  "empty_no_skills": "未找到 SKILL.md。", "empty_hint": "每个 skill 是一个包含 SKILL.md 的文件夹。",
  "capped_notice": "仅显示前 {{count}} 个；实际更多。",
  "folder_too_large": "文件夹过大，浏览器无法读取。",
  "discover_failed": "无法读取该来源。",
  "import_selected": "导入选中的 {{count}} 个"
}
```

`ko/skills.json`:
```jsonc
"bulk": { "title": "일괄 가져오기", "desc": "GitHub 저장소나 로컬 폴더에서 여러 skill을 한 번에 가져옵니다." }
"bulk_title": "일괄 가져오기",
"bulk_desc": "여러 skill을 한 번에 — GitHub 저장소 또는 로컬 폴더.",
"bulk_import": {
  "source_label": "소스", "source_github": "GitHub 저장소", "source_folder": "로컬 폴더",
  "github_url_label": "저장소 또는 폴더 URL",
  "github_url_placeholder": "github.com/owner/repo 또는 .../tree/main/skills",
  "discover": "찾기", "discovering": "찾는 중…",
  "folder_pick": "폴더 선택", "folder_drop_hint": "또는 폴더를 여기에 놓으세요",
  "folder_reading": "폴더 읽는 중…",
  "candidates_title": "찾은 skill", "already_exists": "이미 있음",
  "empty_no_skills": "SKILL.md를 찾을 수 없습니다.", "empty_hint": "각 skill은 SKILL.md가 있는 폴더입니다.",
  "capped_notice": "처음 {{count}}개만 표시; 더 있습니다.",
  "folder_too_large": "폴더가 너무 커서 브라우저에서 읽을 수 없습니다.",
  "discover_failed": "해당 소스를 읽을 수 없습니다.",
  "import_selected": "선택한 {{count}}개 가져오기"
}
```

- [ ] **Step 3: Run the parity test — verify it PASSES**

Run: `pnpm --filter @multica/views exec vitest run locales/parity.test.ts`
Expected: PASS (all four locales carry the same keys).

- [ ] **Step 4: Commit**

```bash
git add packages/views/locales/*/skills.json
git commit -m "i18n(skills): add bulk-import keys across locales"
```

---

## Task 2: SKILL.md frontmatter parser (client)

**Files:**
- Create: `packages/views/skills/lib/frontmatter.ts`
- Test: `packages/views/skills/lib/frontmatter.test.ts`

Mirrors the server `parseSkillFrontmatter` (skill.go) so the folder path can read name/description client-side.

- [ ] **Step 1: Write the failing test**

```ts
// packages/views/skills/lib/frontmatter.test.ts
import { describe, expect, it } from "vitest";
import { parseFrontmatter } from "./frontmatter";

describe("parseFrontmatter", () => {
  it("extracts name and description", () => {
    const md = `---\nname: code-reviewer\ndescription: Reviews PRs\n---\n# Body`;
    expect(parseFrontmatter(md)).toEqual({ name: "code-reviewer", description: "Reviews PRs" });
  });
  it("strips surrounding quotes", () => {
    const md = `---\nname: "my skill"\ndescription: 'does X'\n---`;
    expect(parseFrontmatter(md)).toEqual({ name: "my skill", description: "does X" });
  });
  it("returns empty strings when no frontmatter", () => {
    expect(parseFrontmatter("# Just a heading")).toEqual({ name: "", description: "" });
  });
  it("returns empty strings when frontmatter is unterminated", () => {
    expect(parseFrontmatter("---\nname: x\nstill going")).toEqual({ name: "", description: "" });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @multica/views exec vitest run skills/lib/frontmatter.test.ts`
Expected: FAIL ("parseFrontmatter is not a function" / module not found).

- [ ] **Step 3: Write the implementation**

```ts
// packages/views/skills/lib/frontmatter.ts
export type Frontmatter = { name: string; description: string };

// Mirrors server parseSkillFrontmatter (server/internal/handler/skill.go):
// only the leading `--- ... ---` block, line-prefixed name:/description:.
export function parseFrontmatter(content: string): Frontmatter {
  const result: Frontmatter = { name: "", description: "" };
  if (!content.startsWith("---")) return result;
  const end = content.indexOf("---", 3);
  if (end < 0) return result;
  const block = content.slice(3, end);
  for (const raw of block.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("name:")) {
      result.name = stripQuotes(line.slice("name:".length).trim());
    } else if (line.startsWith("description:")) {
      result.description = stripQuotes(line.slice("description:".length).trim());
    }
  }
  return result;
}

function stripQuotes(s: string): string {
  return s.replace(/^["']|["']$/g, "");
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm --filter @multica/views exec vitest run skills/lib/frontmatter.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/views/skills/lib/frontmatter.ts packages/views/skills/lib/frontmatter.test.ts
git commit -m "feat(skills): client SKILL.md frontmatter parser"
```

---

## Task 3: Folder discovery util (client)

**Files:**
- Create: `packages/views/skills/lib/folder-discovery.ts`
- Test: `packages/views/skills/lib/folder-discovery.test.ts`

Turns a browser `File[]` (each with `webkitRelativePath`) into `FolderCandidate[]`: every `SKILL.md` is one skill; sibling/descendant files up to the next `SKILL.md` are its files; binaries and over-cap files are skipped.

- [ ] **Step 1: Write the failing test**

```ts
// packages/views/skills/lib/folder-discovery.test.ts
import { describe, expect, it } from "vitest";
import { discoverFolderSkills, type FolderEntry } from "./folder-discovery";

function entry(path: string, content: string): FolderEntry {
  return { relativePath: path, text: async () => content, size: content.length };
}

describe("discoverFolderSkills", () => {
  it("finds each SKILL.md as a skill and groups sibling files", async () => {
    const entries = [
      entry("root/skills/a/SKILL.md", "---\nname: a\ndescription: A\n---"),
      entry("root/skills/a/ref.md", "hello"),
      entry("root/skills/b/SKILL.md", "---\nname: b\n---"),
    ];
    const { candidates, truncated } = await discoverFolderSkills(entries);
    expect(truncated).toBe(false);
    expect(candidates.map((c) => c.name).sort()).toEqual(["a", "b"]);
    const a = candidates.find((c) => c.name === "a")!;
    expect(a.data.files?.map((f) => f.path)).toEqual(["ref.md"]);
    expect(a.data.content).toContain("name: a");
  });

  it("falls back to the directory name when frontmatter has no name", async () => {
    const { candidates } = await discoverFolderSkills([
      entry("x/my-skill/SKILL.md", "# no frontmatter"),
    ]);
    expect(candidates[0]!.name).toBe("my-skill");
  });

  it("skips binary files but keeps the skill", async () => {
    const { candidates } = await discoverFolderSkills([
      entry("s/SKILL.md", "---\nname: s\n---"),
      entry("s/logo.png", "BINARY"),
    ]);
    expect(candidates[0]!.data.files ?? []).toEqual([]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @multica/views exec vitest run skills/lib/folder-discovery.test.ts`
Expected: FAIL (module not found).

- [ ] **Step 3: Write the implementation**

```ts
// packages/views/skills/lib/folder-discovery.ts
import type { CreateSkillRequest } from "@multica/core/types";
import { parseFrontmatter } from "./frontmatter";

// Mirror the server caps (server/internal/handler/skill.go).
export const MAX_FILE_SIZE = 1 << 20; // 1 MiB
export const MAX_TOTAL_SIZE = 8 << 20; // 8 MiB per skill
export const MAX_FILE_COUNT = 128; // supporting files per skill
export const MAX_CANDIDATES = 100; // per bulk operation

// Mirror server isLikelyBinaryFilePath extension list.
const BINARY_EXT = new Set([
  "png", "jpg", "jpeg", "gif", "webp", "ico", "bmp", "svg", "pdf", "zip",
  "gz", "tar", "exe", "dll", "so", "dylib", "mp4", "mov", "mp3", "wav",
  "woff", "woff2", "ttf", "otf", "bin", "wasm",
]);

export type FolderEntry = {
  relativePath: string; // webkitRelativePath, e.g. "root/skills/a/SKILL.md"
  size: number;
  text: () => Promise<string>;
};

export type FolderCandidate = {
  name: string;
  description: string;
  path: string; // dir containing SKILL.md
  data: CreateSkillRequest;
};

export type FolderDiscovery = {
  candidates: FolderCandidate[];
  truncated: boolean;
};

function isBinary(path: string): boolean {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return BINARY_EXT.has(ext);
}

export async function discoverFolderSkills(
  entries: FolderEntry[],
): Promise<FolderDiscovery> {
  // Each SKILL.md anchors a skill at its directory.
  const skillDirs = entries
    .filter((e) => e.relativePath.endsWith("/SKILL.md") || e.relativePath === "SKILL.md")
    .map((e) => e.relativePath.replace(/SKILL\.md$/, "")); // keeps trailing slash or ""

  // Sort longest-prefix-first so a nested skill claims its files before an
  // ancestor skill does.
  const dirsByDepth = [...skillDirs].sort((a, b) => b.length - a.length);

  const candidates: FolderCandidate[] = [];
  const truncated = skillDirs.length > MAX_CANDIDATES;

  for (const dir of skillDirs.slice(0, MAX_CANDIDATES)) {
    const mdEntry = entries.find((e) => e.relativePath === dir + "SKILL.md")!;
    const content = await mdEntry.text();
    const fm = parseFrontmatter(content);
    const dirName = dir.replace(/\/$/, "").split("/").pop() || "skill";
    const name = fm.name || dirName;

    const files: { path: string; content: string }[] = [];
    let total = content.length;
    for (const e of entries) {
      if (!e.relativePath.startsWith(dir)) continue;
      if (e.relativePath === dir + "SKILL.md") continue;
      // Claim a file only if THIS dir is its nearest SKILL.md ancestor.
      const nearest = dirsByDepth.find((d) => e.relativePath.startsWith(d));
      if (nearest !== dir) continue;
      const rel = e.relativePath.slice(dir.length);
      if (isBinary(rel)) continue;
      if (e.size > MAX_FILE_SIZE) continue;
      if (files.length >= MAX_FILE_COUNT) continue;
      if (total + e.size > MAX_TOTAL_SIZE) continue;
      files.push({ path: rel, content: await e.text() });
      total += e.size;
    }

    candidates.push({
      name,
      description: fm.description,
      path: dir.replace(/\/$/, "") || "(root)",
      data: { name, description: fm.description, content, files },
    });
  }

  return { candidates, truncated };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm --filter @multica/views exec vitest run skills/lib/folder-discovery.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/views/skills/lib/folder-discovery.ts packages/views/skills/lib/folder-discovery.test.ts
git commit -m "feat(skills): discover skills from an uploaded folder"
```

---

## Task 4: Shared bulk-import engine hook

**Files:**
- Create: `packages/views/skills/hooks/use-bulk-skill-import.ts`
- Test: `packages/views/skills/hooks/use-bulk-skill-import.test.ts`

A source-agnostic engine. Tasks are either `{kind:"payload"}` (folder → `createSkill`) or `{kind:"url"}` (GitHub → `importSkill`). Copies the 10-wide concurrency pool from the runtime panel; 409 → `skipped`; invalidates once + seeds detail caches.

- [ ] **Step 1: Write the failing test (pure runner, no React)**

Extract the pool into a pure `runBulkImport` so it is unit-testable without a renderer.

```ts
// packages/views/skills/hooks/use-bulk-skill-import.test.ts
import { describe, expect, it, vi } from "vitest";
import { runBulkImport, type BulkTask } from "./use-bulk-skill-import";

const skill = (id: string) => ({ id, name: id }) as any;

describe("runBulkImport", () => {
  it("imports payload + url tasks and reports success", async () => {
    const tasks: BulkTask[] = [
      { key: "a", name: "a", kind: "payload", data: { name: "a" } },
      { key: "b", name: "b", kind: "url", url: "u", importName: "b" },
    ];
    const deps = {
      createSkill: vi.fn(async () => skill("a")),
      importSkill: vi.fn(async () => skill("b")),
      onProgress: vi.fn(),
    };
    const results = await runBulkImport(tasks, deps, { current: false });
    expect(results.map((r) => r.status)).toEqual(["success", "success"]);
    expect(deps.createSkill).toHaveBeenCalledOnce();
    expect(deps.importSkill).toHaveBeenCalledOnce();
  });

  it("maps a 409 conflict to skipped, other errors to failed", async () => {
    const tasks: BulkTask[] = [
      { key: "a", name: "a", kind: "payload", data: { name: "a" } },
      { key: "b", name: "b", kind: "payload", data: { name: "b" } },
    ];
    const deps = {
      createSkill: vi
        .fn()
        .mockRejectedValueOnce(new Error("409 already exists"))
        .mockRejectedValueOnce(new Error("network boom")),
      importSkill: vi.fn(),
      onProgress: vi.fn(),
    };
    const results = await runBulkImport(tasks, deps, { current: false });
    expect(results.find((r) => r.key === "a")!.status).toBe("skipped");
    expect(results.find((r) => r.key === "b")!.status).toBe("failed");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @multica/views exec vitest run skills/hooks/use-bulk-skill-import.test.ts`
Expected: FAIL (module not found).

- [ ] **Step 3: Write the implementation**

```ts
// packages/views/skills/hooks/use-bulk-skill-import.ts
import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { CreateSkillRequest, Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillDetailOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { isNameConflictError } from "../lib/utils";

const IMPORT_CONCURRENCY = 10;

export type BulkTask =
  | { key: string; name: string; kind: "payload"; data: CreateSkillRequest }
  | { key: string; name: string; kind: "url"; url: string; importName: string };

export type BulkResult = {
  key: string;
  name: string;
  status: "success" | "skipped" | "failed";
  error?: string;
  skill?: Skill;
};

export type BulkPhase = "idle" | "importing" | "done" | "cancelled";

type Deps = {
  createSkill: (d: CreateSkillRequest) => Promise<Skill>;
  importSkill: (d: { url: string }) => Promise<Skill>;
  onProgress: (r: BulkResult[]) => void;
};

// Pure runner — testable without React. 10-wide pool; partial-success.
export async function runBulkImport(
  tasks: BulkTask[],
  deps: Deps,
  cancelRef: { current: boolean },
): Promise<BulkResult[]> {
  const results: BulkResult[] = [];

  const importOne = async (task: BulkTask) => {
    try {
      const skill =
        task.kind === "payload"
          ? await deps.createSkill(task.data)
          : await deps.importSkill({ url: task.url });
      results.push({ key: task.key, name: task.name, status: "success", skill });
    } catch (err) {
      const msg = err instanceof Error ? err.message : "";
      results.push({
        key: task.key,
        name: task.name,
        status: isNameConflictError(msg) ? "skipped" : "failed",
        error: msg,
      });
    }
    deps.onProgress([...results]);
  };

  const executing = new Set<Promise<void>>();
  for (const task of tasks) {
    if (cancelRef.current) break;
    const p = importOne(task).then(() => {
      executing.delete(p);
    });
    executing.add(p);
    if (executing.size >= IMPORT_CONCURRENCY) {
      await Promise.race(executing);
    }
  }
  await Promise.all(executing);
  return results;
}

// React wrapper — owns phase/progress state + query invalidation.
export function useBulkSkillImport() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const cancelRef = useRef(false);
  const [phase, setPhase] = useState<BulkPhase>("idle");
  const [total, setTotal] = useState(0);
  const [results, setResults] = useState<BulkResult[]>([]);

  const start = async (tasks: BulkTask[]) => {
    cancelRef.current = false;
    setTotal(tasks.length);
    setResults([]);
    setPhase("importing");

    const finalResults = await runBulkImport(
      tasks,
      {
        createSkill: api.createSkill.bind(api),
        importSkill: api.importSkill.bind(api),
        onProgress: setResults,
      },
      cancelRef,
    );

    await Promise.all([
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);
    for (const r of finalResults) {
      if (r.status === "success" && r.skill) {
        qc.setQueryData(skillDetailOptions(wsId, r.skill.id).queryKey, r.skill);
      }
    }
    setPhase(cancelRef.current ? "cancelled" : "done");
  };

  const cancel = () => {
    cancelRef.current = true;
  };

  return { phase, total, results, completed: results.length, start, cancel };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm --filter @multica/views exec vitest run skills/hooks/use-bulk-skill-import.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/views/skills/hooks/use-bulk-skill-import.ts packages/views/skills/hooks/use-bulk-skill-import.test.ts
git commit -m "feat(skills): shared bulk-import engine"
```

---

## Task 5: Bulk import panel (Folder source) + wire into dialog

**Files:**
- Create: `packages/views/skills/components/bulk-skill-import-panel.tsx`
- Modify: `packages/views/skills/components/create-skill-dialog.tsx`

This task ships a working "import from local folder". GitHub source is added in Phase 2 (the toggle is present but the GitHub branch is stubbed to call a not-yet-wired discover — guarded so Folder works standalone).

- [ ] **Step 1: Create the panel component**

```tsx
// packages/views/skills/components/bulk-skill-import-panel.tsx
"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle, CheckCircle2, Download, FolderUp, Loader2, SkipForward,
} from "lucide-react";
import type { Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Progress } from "@multica/ui/components/ui/progress";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useT } from "../../i18n";
import {
  discoverFolderSkills, type FolderCandidate, type FolderEntry,
} from "../lib/folder-discovery";
import {
  useBulkSkillImport, type BulkResult, type BulkTask,
} from "../hooks/use-bulk-skill-import";

type Source = "folder" | "github";

// A unified candidate the checklist renders, regardless of source.
type Candidate = {
  key: string;
  name: string;
  description: string;
  path: string;
  fileCount: number;
  toTask: () => BulkTask;
};

function folderToCandidate(c: FolderCandidate): Candidate {
  return {
    key: c.path + "::" + c.name,
    name: c.name,
    description: c.description,
    path: c.path,
    fileCount: (c.data.files?.length ?? 0) + 1,
    toTask: () => ({ key: c.path + "::" + c.name, name: c.name, kind: "payload", data: c.data }),
  };
}

export function BulkSkillImportPanel({
  onBulkDone,
}: {
  onImported?: (skill: Skill) => void;
  onBulkDone?: () => void;
}) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const [source, setSource] = useState<Source>("folder");
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reading, setReading] = useState(false);
  const [error, setError] = useState("");
  const folderInputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const { phase, total, results, completed, start, cancel } = useBulkSkillImport();
  const importing = phase === "importing";

  // Existing names → "already exists" badge + default-unchecked.
  const { data: existing = [] } = useQuery(skillListOptions(wsId));
  const existingNames = useMemo(
    () => new Set(existing.map((s) => s.name.toLowerCase())),
    [existing],
  );

  const applyCandidates = (list: Candidate[], wasTruncated: boolean) => {
    setCandidates(list);
    setTruncated(wasTruncated);
    setSelected(
      new Set(list.filter((c) => !existingNames.has(c.name.toLowerCase())).map((c) => c.key)),
    );
  };

  const onFolderPicked = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) return;
    setReading(true);
    setError("");
    try {
      const entries: FolderEntry[] = Array.from(fileList).map((f) => ({
        relativePath: (f as File & { webkitRelativePath?: string }).webkitRelativePath || f.name,
        size: f.size,
        text: () => f.text(),
      }));
      const { candidates: fc, truncated: tr } = await discoverFolderSkills(entries);
      applyCandidates(fc.map(folderToCandidate), tr);
    } catch {
      setError(t(($) => $.bulk_import.discover_failed));
    } finally {
      setReading(false);
    }
  };

  const toggle = (key: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  const allSelected = candidates.length > 0 && selected.size === candidates.length;
  const toggleAll = () =>
    setSelected(allSelected ? new Set() : new Set(candidates.map((c) => c.key)));

  const handleImport = () => {
    const tasks = candidates.filter((c) => selected.has(c.key)).map((c) => c.toTask());
    if (tasks.length) start(tasks);
  };

  const middle = (() => {
    if (importing) {
      const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
      return (
        <div className="space-y-4 py-4">
          <div className="text-center">
            <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
            <p className="mt-3 text-sm font-medium">
              {t(($) => $.runtime_import.bulk_progress, { completed, total })}
            </p>
          </div>
          <Progress value={pct} />
        </div>
      );
    }
    if (phase === "done" || phase === "cancelled") {
      return <Summary results={results} />;
    }
    if (reading) {
      return (
        <div className="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          {t(($) => $.bulk_import.folder_reading)}
        </div>
      );
    }
    if (candidates.length === 0) {
      return (
        <button
          type="button"
          onClick={() => folderInputRef.current?.click()}
          className="flex w-full flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-12 text-center hover:bg-accent/40"
        >
          <FolderUp className="h-6 w-6 text-muted-foreground" />
          <span className="text-sm font-medium">{t(($) => $.bulk_import.folder_pick)}</span>
          <span className="text-xs text-muted-foreground">
            {t(($) => $.bulk_import.folder_drop_hint)}
          </span>
        </button>
      );
    }
    return (
      <div className="space-y-2">
        {truncated && (
          <p className="rounded-md bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
            {t(($) => $.bulk_import.capped_notice, { count: candidates.length })}
          </p>
        )}
        <label className="flex cursor-pointer items-center gap-2 px-1 py-1">
          <input type="checkbox" checked={allSelected} onChange={toggleAll}
            className="cursor-pointer accent-primary" />
          <span className="text-xs text-muted-foreground">
            {t(($) => $.runtime_import.select_all, { count: candidates.length })}
          </span>
        </label>
        {candidates.map((c) => {
          const exists = existingNames.has(c.name.toLowerCase());
          return (
            <div key={c.key}
              role="button" tabIndex={0} onClick={() => toggle(c.key)}
              className={`flex items-start gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                selected.has(c.key) ? "border-primary bg-primary/5" : "hover:bg-accent/40"
              }`}>
              <Checkbox checked={selected.has(c.key)} tabIndex={-1} className="pointer-events-none mt-0.5" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium">{c.name}</span>
                  {exists && <Badge variant="outline">{t(($) => $.bulk_import.already_exists)}</Badge>}
                </div>
                {c.description && (
                  <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{c.description}</p>
                )}
                <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{c.path}</p>
              </div>
            </div>
          );
        })}
      </div>
    );
  })();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Source toggle */}
      <div className={`shrink-0 space-y-2 border-b px-5 py-3 ${importing ? "pointer-events-none opacity-60" : ""}`}>
        <span className="text-xs text-muted-foreground">{t(($) => $.bulk_import.source_label)}</span>
        <div className="flex gap-2">
          {(["folder", "github"] as Source[]).map((s) => (
            <button key={s} type="button" onClick={() => { setSource(s); setCandidates([]); setError(""); }}
              className={`rounded-md border px-3 py-1.5 text-xs ${source === s ? "border-primary bg-primary/5 font-medium" : "text-muted-foreground"}`}>
              {s === "folder" ? t(($) => $.bulk_import.source_folder) : t(($) => $.bulk_import.source_github)}
            </button>
          ))}
        </div>
        {source === "github" && (
          <p className="text-xs text-muted-foreground">{t(($) => $.bulk_import.empty_hint)}</p>
        )}
      </div>

      <input ref={folderInputRef} type="file" hidden multiple
        // @ts-expect-error webkitdirectory is non-standard but supported in Chromium/Electron
        webkitdirectory="" directory=""
        onChange={(e) => onFolderPicked(e.target.files)} />

      <div ref={scrollRef} style={fadeStyle} className="min-h-0 flex-1 overflow-y-auto px-5 py-3">
        {error && (
          <div role="alert" className="mb-2 flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{error}
          </div>
        )}
        {middle}
      </div>

      {/* Footer */}
      <div className="flex shrink-0 items-center gap-3 border-t bg-muted/30 px-5 py-3">
        {phase === "done" || phase === "cancelled" ? (
          <>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              {t(($) => $.runtime_import.bulk_complete_hint)}
            </div>
            <Button type="button" size="sm" onClick={onBulkDone}>
              {t(($) => $.runtime_import.bulk_done_button)}
            </Button>
          </>
        ) : importing ? (
          <>
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              {t(($) => $.runtime_import.bulk_progress, { completed, total })}
            </div>
            <Button type="button" size="sm" variant="outline" onClick={cancel}>
              {t(($) => $.runtime_import.bulk_cancel_button)}
            </Button>
          </>
        ) : (
          <>
            <div className="min-w-0 flex-1" />
            <Button type="button" size="sm" disabled={selected.size === 0} onClick={handleImport}>
              <Download className="h-3 w-3" />
              {t(($) => $.bulk_import.import_selected, { count: selected.size })}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

function Summary({ results }: { results: BulkResult[] }) {
  const { t } = useT("skills");
  const by = (s: BulkResult["status"]) => results.filter((r) => r.status === s);
  return (
    <div className="space-y-4 py-2">
      <div className="grid grid-cols-3 gap-2 text-center">
        <Counter n={by("success").length} label={t(($) => $.runtime_import.bulk_summary_imported)} tone="green" />
        <Counter n={by("skipped").length} label={t(($) => $.runtime_import.bulk_summary_skipped)} tone="yellow" />
        <Counter n={by("failed").length} label={t(($) => $.runtime_import.bulk_summary_failed)} tone="red" />
      </div>
      <div className="max-h-64 space-y-1 overflow-y-auto rounded-md border p-2">
        {results.map((r) => (
          <div key={r.key} className="flex items-center gap-2 rounded px-2 py-1.5 text-xs">
            {r.status === "success" && <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-600" />}
            {r.status === "skipped" && <SkipForward className="h-3.5 w-3.5 shrink-0 text-yellow-600" />}
            {r.status === "failed" && <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />}
            <span className="min-w-0 flex-1 truncate">{r.name}</span>
            {r.error && <span className="max-w-[200px] shrink-0 truncate text-muted-foreground">{r.error}</span>}
          </div>
        ))}
      </div>
    </div>
  );
}

function Counter({ n, label, tone }: { n: number; label: string; tone: "green" | "yellow" | "red" }) {
  const cls = {
    green: "bg-green-50 text-green-700 dark:bg-green-950/30 dark:text-green-400",
    yellow: "bg-yellow-50 text-yellow-700 dark:bg-yellow-950/30 dark:text-yellow-400",
    red: "bg-red-50 text-red-700 dark:bg-red-950/30 dark:text-red-400",
  }[tone];
  return (
    <div className={`rounded-md px-3 py-2 ${cls}`}>
      <div className="text-lg font-semibold">{n}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}
```

- [ ] **Step 2: Wire the `bulk` method into the dialog**

In `packages/views/skills/components/create-skill-dialog.tsx`:

Add the import near the others (line ~42):
```tsx
import { BulkSkillImportPanel } from "./bulk-skill-import-panel";
import { Layers } from "lucide-react"; // add to the existing lucide-react import block
```

Extend the `Method` type (line 46):
```tsx
type Method = "chooser" | "manual" | "url" | "runtime" | "bulk";
```

Add the chooser card (inside the `methods` array, after `runtime`):
```tsx
{ key: "bulk", icon: Layers, titleKey: "bulk" },
```
and widen the `titleKey` union on that array to include `"bulk"`.

Make the dialog wide for bulk (line 442):
```tsx
const wide = method === "runtime" || method === "bulk";
```

Render the panel (after the `runtime` block, ~line 521):
```tsx
{method === "bulk" && (
  <BulkSkillImportPanel onImported={handleCreated} onBulkDone={onClose} />
)}
```

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter @multica/views typecheck`
Expected: PASS (no type errors).

- [ ] **Step 4: Manual verification (folder path)**

Rebuild the frontend and exercise it:
```bash
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build --force-recreate frontend
```
Open Skills → New skill → **Import a set** → Local folder → choose a folder that has two `*/SKILL.md` skills → confirm both appear, one duplicate shows "already exists" + unchecked, import → summary shows imported/skipped.

- [ ] **Step 5: Commit**

```bash
git add packages/views/skills/components/bulk-skill-import-panel.tsx packages/views/skills/components/create-skill-dialog.tsx
git commit -m "feat(skills): bulk import panel with local-folder source"
```

---

# PHASE 2 — GitHub repo/folder import

## Task 6: Backend GitHub skill discovery

**Files:**
- Create: `server/internal/handler/skill_discover.go`
- Test: `server/internal/handler/skill_discover_test.go`

Reuses `detectImportSource`, `parseGitHubURL`, `resolveGitHubRefAndPath`, `fetchGitHubDefaultBranch`, `doGitHubAPIGet`, `parseSkillFrontmatter`, `fetchRawFile`, `buildRawGitHubURL`, `escapeRefPath` from `skill.go`. Adds one Git Trees API call to enumerate every `SKILL.md`.

- [ ] **Step 1: Write the failing unit test (pure discovery over a fake tree)**

Make discovery testable by injecting the tree-fetch and md-fetch as function params.

```go
// server/internal/handler/skill_discover_test.go
package handler

import (
	"testing"
)

func TestSelectSkillDirsFromTree(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "skills/a/SKILL.md", Type: "blob"},
		{Path: "skills/a/ref.md", Type: "blob"},
		{Path: "skills/b/SKILL.md", Type: "blob"},
		{Path: "README.md", Type: "blob"},
		{Path: "skills/a", Type: "tree"},
	}
	dirs := selectSkillDirsFromTree(tree, "")
	if len(dirs) != 2 {
		t.Fatalf("want 2 skill dirs, got %d: %v", len(dirs), dirs)
	}
	if dirs[0] != "skills/a" || dirs[1] != "skills/b" {
		t.Fatalf("unexpected dirs: %v", dirs)
	}
}

func TestSelectSkillDirsScopedToSubdir(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "skills/a/SKILL.md", Type: "blob"},
		{Path: "other/c/SKILL.md", Type: "blob"},
	}
	dirs := selectSkillDirsFromTree(tree, "skills")
	if len(dirs) != 1 || dirs[0] != "skills/a" {
		t.Fatalf("scoping failed: %v", dirs)
	}
}

func TestSelectSkillDirsRootSkill(t *testing.T) {
	tree := []githubTreeEntry{{Path: "SKILL.md", Type: "blob"}}
	dirs := selectSkillDirsFromTree(tree, "")
	if len(dirs) != 1 || dirs[0] != "" {
		t.Fatalf("root skill not detected: %v", dirs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/handler/ -run TestSelectSkillDirs`
Expected: FAIL (undefined: `selectSkillDirsFromTree`, `githubTreeEntry`).

- [ ] **Step 3: Write the discovery implementation**

```go
// server/internal/handler/skill_discover.go
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
)

// maxDiscoverCandidates caps how many skills one discovery returns. Mirrors
// MAX_CANDIDATES on the client (packages/views/skills/lib/folder-discovery.ts).
const maxDiscoverCandidates = 100

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" | "tree"
}

type githubTreeResponse struct {
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

// SkillCandidate is one discovered skill the client can import by URL.
type SkillCandidate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	ImportURL   string `json:"import_url"`
}

type SkillDiscoveryResponse struct {
	Candidates []SkillCandidate `json:"candidates"`
	Truncated  bool             `json:"truncated"`
}

// selectSkillDirsFromTree returns the directory of every SKILL.md blob in the
// tree, scoped under `scope` (""=whole repo). "" means a root SKILL.md.
func selectSkillDirsFromTree(tree []githubTreeEntry, scope string) []string {
	scope = strings.Trim(scope, "/")
	dirs := make([]string, 0)
	for _, e := range tree {
		if e.Type != "blob" {
			continue
		}
		if e.Path != "SKILL.md" && !strings.HasSuffix(e.Path, "/SKILL.md") {
			continue
		}
		dir := strings.TrimSuffix(strings.TrimSuffix(e.Path, "SKILL.md"), "/")
		if scope != "" && dir != scope && !strings.HasPrefix(dir, scope+"/") {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// discoverGitHubSkills resolves the repo/ref, fetches the recursive tree, and
// for each SKILL.md reads its frontmatter to build a candidate list. Only the
// SKILL.md bodies are fetched here (cheap) — supporting files are fetched at
// import time by the existing single-skill importer.
func (h *Handler) discoverGitHubSkills(rawURL string) (*SkillDiscoveryResponse, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	spec, err := parseGitHubURL(rawURL)
	if err != nil {
		return nil, err
	}
	if len(spec.refSegments) > 0 {
		if err := resolveGitHubRefAndPath(httpClient, &spec); err != nil {
			return nil, err
		}
	}
	if spec.ref == "" {
		spec.ref = fetchGitHubDefaultBranch(httpClient, spec.owner, spec.repo)
	}

	treeURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		spec.owner, spec.repo, escapeRefPath(spec.ref),
	)
	resp, err := doGitHubAPIGet(httpClient, treeURL)
	if err != nil {
		return nil, fmt.Errorf("failed to reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status %d listing the repository tree", resp.StatusCode)
	}
	var tree githubTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub tree response")
	}

	dirs := selectSkillDirsFromTree(tree.Tree, spec.skillDir)
	truncated := tree.Truncated || len(dirs) > maxDiscoverCandidates
	if len(dirs) > maxDiscoverCandidates {
		dirs = dirs[:maxDiscoverCandidates]
	}

	rawPrefix := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s",
		spec.owner, spec.repo, escapeRefPath(spec.ref))

	candidates := make([]SkillCandidate, 0, len(dirs))
	for _, dir := range dirs {
		mdPath := "SKILL.md"
		if dir != "" {
			mdPath = dir + "/SKILL.md"
		}
		body, err := fetchRawFile(httpClient, buildRawGitHubURL(rawPrefix, mdPath))
		if err != nil {
			continue // unreadable/oversize SKILL.md → skip this candidate
		}
		name, description := parseSkillFrontmatter(string(body))
		if name == "" {
			if dir == "" {
				name = spec.repo
			} else {
				name = path.Base(dir)
			}
		}
		importURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s",
			spec.owner, spec.repo, spec.ref)
		if dir != "" {
			importURL += "/" + dir
		}
		display := dir
		if display == "" {
			display = "(root)"
		}
		candidates = append(candidates, SkillCandidate{
			Name:        name,
			Description: description,
			Path:        display,
			ImportURL:   importURL,
		})
	}

	return &SkillDiscoveryResponse{Candidates: candidates, Truncated: truncated}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/handler/ -run TestSelectSkillDirs`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/skill_discover.go server/internal/handler/skill_discover_test.go
git commit -m "feat(skills): GitHub skill discovery over the tree API"
```

---

## Task 7: DiscoverSkills HTTP handler + route

**Files:**
- Modify: `server/internal/handler/skill_discover.go` (add handler)
- Modify: `server/cmd/server/router.go` (register route)
- Test: `server/internal/handler/skill_discover_test.go` (add handler test)

- [ ] **Step 1: Write the failing handler test**

```go
// add to server/internal/handler/skill_discover_test.go
import (
	"net/http"
	"net/http/httptest"
	"strings"
)

func TestDiscoverSkillsRejectsNonGitHub(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/skills/discover",
		strings.NewReader(`{"url":"https://clawhub.ai/owner/skill"}`))
	req.Header.Set("Content-Type", "application/json")
	testHandler.DiscoverSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-GitHub url, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDiscoverSkillsRejectsInvalidBody(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/skills/discover", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	testHandler.DiscoverSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad body, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/handler/ -run TestDiscoverSkills`
Expected: FAIL (undefined: `testHandler.DiscoverSkills`).

- [ ] **Step 3: Add the handler**

Append to `server/internal/handler/skill_discover.go`:

```go
// DiscoverSkills lists the skills found under a GitHub repo/folder URL without
// importing them. GitHub-only; other sources return 400. Workspace membership
// is enforced by the /api/skills route group middleware.
func (h *Handler) DiscoverSkills(w http.ResponseWriter, r *http.Request) {
	var req ImportSkillRequest // reuse the { "url": ... } body shape
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	source, normalized, err := detectImportSource(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if source != sourceGitHub {
		writeError(w, http.StatusBadRequest, "only GitHub repositories are supported for bulk discovery")
		return
	}

	result, err := h.discoverGitHubSkills(normalized)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 4: Register the route**

In `server/cmd/server/router.go`, inside the `r.Route("/api/skills", ...)` block (next to `r.Post("/import", h.ImportSkill)`), add:

```go
r.Post("/discover", h.DiscoverSkills)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./internal/handler/ -run TestDiscoverSkills`
Expected: PASS.
Run: `cd server && go vet ./internal/handler/ && go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/skill_discover.go server/internal/handler/skill_discover_test.go server/cmd/server/router.go
git commit -m "feat(skills): POST /api/skills/discover endpoint"
```

---

## Task 8: Client `discoverSkills` method + types

**Files:**
- Modify: `packages/core/types/agent.ts`
- Modify: `packages/core/api/client.ts`

- [ ] **Step 1: Add the types**

In `packages/core/types/agent.ts` (next to the skill types):

```ts
export interface SkillCandidate {
  name: string;
  description: string;
  path: string;
  import_url: string;
}

export interface SkillDiscoveryResult {
  candidates: SkillCandidate[];
  truncated: boolean;
}
```

Ensure they are re-exported wherever the other skill types are exported from `@multica/core/types` (mirror the export of `SkillSummary`).

- [ ] **Step 2: Add the client method**

In `packages/core/api/client.ts`, next to `importSkill` (line ~1490):

```ts
async discoverSkills(data: { url: string }): Promise<SkillDiscoveryResult> {
  return this.fetch("/api/skills/discover", {
    method: "POST",
    body: JSON.stringify(data),
  });
}
```

Add `SkillDiscoveryResult` to the type import block at the top of `client.ts` (alongside `Skill`, `SkillSummary`).

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter @multica/core typecheck`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add packages/core/types/agent.ts packages/core/api/client.ts
git commit -m "feat(api): discoverSkills client method + types"
```

---

## Task 9: Wire the GitHub source into the bulk panel

**Files:**
- Modify: `packages/views/skills/components/bulk-skill-import-panel.tsx`

- [ ] **Step 1: Add GitHub discovery state + URL form**

At the top of `BulkSkillImportPanel`, add imports and state:

```tsx
import { api } from "@multica/core/api";
import type { SkillCandidate } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
// ...
const [githubUrl, setGithubUrl] = useState("");
```

Add a converter (next to `folderToCandidate`):

```tsx
function githubToCandidate(c: SkillCandidate): Candidate {
  return {
    key: "gh::" + c.import_url,
    name: c.name,
    description: c.description,
    path: c.path,
    fileCount: 0,
    toTask: () => ({
      key: "gh::" + c.import_url,
      name: c.name,
      kind: "url",
      url: c.import_url,
      importName: c.name,
    }),
  };
}
```

Add the discover handler:

```tsx
const onDiscover = async () => {
  const url = githubUrl.trim();
  if (!url) return;
  setReading(true);
  setError("");
  try {
    const res = await api.discoverSkills({ url });
    if (!res?.candidates?.length) {
      applyCandidates([], false);
      setError(t(($) => $.bulk_import.empty_no_skills));
      return;
    }
    applyCandidates(res.candidates.map(githubToCandidate), res.truncated === true);
  } catch (err) {
    setError(err instanceof Error ? err.message : t(($) => $.bulk_import.discover_failed));
  } finally {
    setReading(false);
  }
};
```

- [ ] **Step 2: Render the GitHub URL input when the source is `github`**

Replace the `source === "github"` hint block in the toggle area with the URL field + Discover button:

```tsx
{source === "github" && (
  <div className="space-y-1.5 pt-1">
    <Input
      value={githubUrl}
      onChange={(e) => { setGithubUrl(e.target.value); setError(""); }}
      placeholder={t(($) => $.bulk_import.github_url_placeholder)}
      className="font-mono text-sm"
      onKeyDown={(e) => { if (e.key === "Enter") onDiscover(); }}
    />
    <div className="flex justify-end">
      <Button type="button" size="sm" variant="outline" disabled={!githubUrl.trim() || reading} onClick={onDiscover}>
        {reading ? (
          <><Loader2 className="h-3 w-3 animate-spin" />{t(($) => $.bulk_import.discovering)}</>
        ) : (
          <><Download className="h-3 w-3" />{t(($) => $.bulk_import.discover)}</>
        )}
      </Button>
    </div>
  </div>
)}
```

Also gate the empty-state folder dropzone so it only renders for the folder source (wrap the `candidates.length === 0` dropzone branch in `source === "folder" && …`; for github with no candidates yet, render nothing / the existing `empty_hint`).

- [ ] **Step 3: Typecheck**

Run: `pnpm --filter @multica/views typecheck`
Expected: PASS.

- [ ] **Step 4: Manual verification (GitHub path)**

```bash
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build --force-recreate frontend backend
```
Skills → New skill → Import a set → GitHub repo → paste a public repo that contains several skills (e.g. a repo with `skills/*/SKILL.md`) → Discover → confirm the candidate list, "already exists" badges, import selected → summary.

- [ ] **Step 5: Commit**

```bash
git add packages/views/skills/components/bulk-skill-import-panel.tsx
git commit -m "feat(skills): wire GitHub source into bulk import panel"
```

---

## Final verification

- [ ] **Run the full check suite**

Run: `make check`
Expected: typecheck, TS unit tests, Go tests, and e2e all pass.

- [ ] **Add the feature branch to the integration build (optional, local)**

If using `update.ps1`, add `'feat/bulk-skill-import'` to its `$FeatureBranches` so the bundle includes it.

---

## Notes & known limitations (from the spec)

- GitHub discovery is subject to GitHub's unauthenticated API rate limit unless `GITHUB_TOKEN` is set on the server (see `doGitHubAPIGet` in `skill.go`). Private repos need that token.
- The folder path reads files into browser memory; the per-file/total/count caps mirror the server, and `MAX_CANDIDATES` (100) bounds the batch with a visible "capped" notice — no silent truncation.
- **Folder drag-drop is deferred.** Phase 1 ships click-to-pick via the
  `webkitdirectory` input (reliable across Chromium/Electron). True folder
  drag-drop needs recursive `webkitGetAsEntry` traversal of the dropped
  directory; it's a follow-up that can reuse `discoverFolderSkills` once the
  entries are flattened. The `folder_drop_hint` copy already anticipates it.
- `task`/`issue`/locale concerns are unrelated to this feature.
