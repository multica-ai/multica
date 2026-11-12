// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { getCommitDiffs, COMMIT_DIFF_TOOL } from "./commit-diff";
import { noopLogger } from "../logger";
import { setSchemaLogger } from "../api/schema";
import type { TaskMessagePayload } from "../types";
const payload = {
  repository: "app",
  repository_id: "repo-1",
  base: "a".repeat(40),
  head: "b".repeat(40),
  changes: [{ path: "a.ts", kind: "modify", diff: "@@ -1 +1 @@\n-old\n+new" }],
};
const message = (input: unknown, seq = 1): TaskMessagePayload => ({
  task_id: "task",
  issue_id: "issue",
  type: "tool_use",
  tool: COMMIT_DIFF_TOOL,
  seq,
  input: input as Record<string, unknown>,
  created_at: "2026-11-11T00:00:00Z",
});
describe("getCommitDiffs", () => {
  it("keeps repository identities separate and selects the newest report", () => {
    const diffs = getCommitDiffs([
      message({ ...payload, head: "c".repeat(40) }, 3),
      message(payload, 1),
      message({ ...payload, repository_id: "repo-2" }, 2),
    ]);
    expect(diffs).toHaveLength(2);
    expect(diffs[0]?.head).toBe("c".repeat(40));
    expect(diffs[0]?.repositories_omitted).toBe(false);
  });
  it("retains the run-level repository limit status", () => {
    const diffs = getCommitDiffs([message({ ...payload, repositories_omitted: true })]);
    expect(diffs[0]?.repositories_omitted).toBe(true);
  });
  it("retains empty and unavailable comparisons alongside the same repository's commits", () => {
    const diffs = getCommitDiffs([
      message(payload),
      message({ ...payload, kind: "worktree", changes: [] }, 2),
      message({ ...payload, kind: "draft", unavailable: true, base: undefined, head: undefined, changes: [] }, 3),
    ]);
    expect(diffs.map(({ kind, unavailable }) => [kind, unavailable])).toEqual([
      ["commits", false],
      ["worktree", false],
      ["draft", true],
    ]);
  });
  it("ignores provider edits and invalid input without logging source code", () => {
    const warn = vi.fn();
    setSchemaLogger({ debug: vi.fn(), info: vi.fn(), warn, error: vi.fn() });
    expect(
      getCommitDiffs([
        message({ ...payload, base: "invalid", source: "secret source" }),
        message({ ...payload, base: undefined }),
        { ...message(payload), tool: "patch_apply" },
      ]),
    ).toEqual([]);
    expect(warn).not.toHaveBeenCalled();
    setSchemaLogger(noopLogger);
  });
  it("retains truncated and binary files even without text", () => {
    const diff = getCommitDiffs([
      message({
        ...payload,
        truncated: true,
        changes: [{ path: "asset.png", kind: "add", binary: true }],
      }),
    ])[0];
    expect(diff?.truncated).toBe(true);
    expect(diff?.changes[0]?.binary).toBe(true);
  });
});
