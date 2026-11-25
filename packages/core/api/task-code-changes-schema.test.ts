// @vitest-environment node
import { describe, expect, it } from "vitest";
import { TaskCodeChangesSchema } from "./schemas";
import { parseWithFallback } from "./schema";
import type { TaskCodeChanges } from "../types";

const FALLBACK: TaskCodeChanges = { task_id: "t", repos: [], repos_truncated: false, updated_at: null };
const parse = (raw: unknown) =>
  parseWithFallback<TaskCodeChanges>(raw, TaskCodeChangesSchema, FALLBACK, {
    endpoint: "GET /api/tasks/:id/code-changes",
  });

const goodRepo = {
  path: "app",
  branch: "agent/fix-login",
  base: "abc",
  head: "def",
  files: [{ path: "main.go", status: "modified", additions: 2, deletions: 1 }],
  patch: "diff --git a/main.go b/main.go\n",
};

describe("TaskCodeChangesSchema", () => {
  it("passes a well-formed snapshot through", () => {
    const got = parse({ task_id: "t", repos: [goodRepo], repos_truncated: true, updated_at: "2026-11-05T10:00:00Z" });
    expect(got.repos).toHaveLength(1);
    expect(got.repos[0]!.files[0]).toMatchObject({ path: "main.go", status: "modified", additions: 2 });
    expect(got.repos_truncated).toBe(true);
    expect(got.updated_at).toBe("2026-11-05T10:00:00Z");
  });

  it("degrades malformed fields instead of dropping the snapshot", () => {
    const got = parse({
      task_id: "t",
      updated_at: 42,
      repos_truncated: "yes",
      repos: [
        {
          ...goodRepo,
          branch: 7,
          patch: null,
          files: [
            { path: "ok.ts", status: "copied", additions: "many", deletions: 1 },
            { status: "added" },
            "not a file",
          ],
        },
        "not a repo",
      ],
    });
    expect(got.updated_at).toBeNull();
    expect(got.repos_truncated).toBe(false);
    expect(got.repos).toHaveLength(1);
    const repo = got.repos[0]!;
    expect(repo.branch).toBeUndefined();
    expect(repo.patch).toBe("");
    expect(repo.files).toEqual([{ path: "ok.ts", status: "modified", additions: 0, deletions: 1 }]);
  });

  it("treats a missing or malformed repo list as no changes", () => {
    expect(parse({ task_id: "t", updated_at: null }).repos).toEqual([]);
    expect(parse({ task_id: "t", repos: "nope", updated_at: null }).repos).toEqual([]);
  });

  it("falls back to an empty snapshot for a response that is not an object", () => {
    expect(parse("oops")).toEqual(FALLBACK);
    expect(parse(null)).toEqual(FALLBACK);
  });
});
