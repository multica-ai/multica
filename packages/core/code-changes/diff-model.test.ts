// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { GitHubPullRequest, TaskCodeChange } from "../types";
import {
  buildDiffFiles,
  groupFilesByDirectory,
  latestLineOfWork,
  pullRequestForChange,
  runNumber,
  splitHunkRows,
} from "./diff-model";
import { parsePatch } from "./parse-patch";

const PATCH = [
  "diff --git a/a.ts b/a.ts",
  "--- a/a.ts",
  "+++ b/a.ts",
  "@@ -1,4 +1,4 @@",
  " keep",
  "-old 1",
  "-old 2",
  "+new 1",
  " keep 2",
  "+tail",
].join("\n");

describe("buildDiffFiles", () => {
  it("uses the patch alone when there is no stored list", () => {
    const files = buildDiffFiles(null, parsePatch(PATCH));
    expect(files).toHaveLength(1);
    expect(files[0]).toMatchObject({ path: "a.ts", additions: 2, deletions: 2 });
    expect(files[0]!.hunks).toHaveLength(1);
  });

  it("keeps the stored list authoritative and marks files the patch lacks", () => {
    const files = buildDiffFiles(
      [
        { path: "a.ts", status: "modified", additions: 2, deletions: 2 },
        { path: "huge.json", status: "added", additions: 90000, deletions: 0 },
        { path: "x", status: "something-new" as never, additions: 1, deletions: 0 },
      ],
      parsePatch(PATCH),
    );
    expect(files.map((f) => [f.path, f.hunks === null])).toEqual([
      ["a.ts", false],
      ["huge.json", true],
      ["x", true],
    ]);
    expect(files[2]!.status).toBe("modified");
  });
});

describe("splitHunkRows", () => {
  it("pairs removals with the additions that replace them", () => {
    const hunk = parsePatch(PATCH)[0]!.hunks[0]!;
    const rows = splitHunkRows(hunk).map((r) => [r.left?.text ?? null, r.right?.text ?? null]);
    expect(rows).toEqual([
      ["keep", "keep"],
      ["old 1", "new 1"],
      ["old 2", null],
      ["keep 2", "keep 2"],
      [null, "tail"],
    ]);
  });
});

describe("groupFilesByDirectory", () => {
  it("groups by directory in path order", () => {
    const groups = groupFilesByDirectory([{ path: "b/z.ts" }, { path: "root.md" }, { path: "b/a.ts" }, { path: "a/x.ts" }]);
    expect(groups.map((g) => [g.directory, g.files.map((f) => f.path)])).toEqual([
      ["a", ["a/x.ts"]],
      ["b", ["b/a.ts", "b/z.ts"]],
      ["", ["root.md"]],
    ]);
  });
});

describe("runNumber", () => {
  it("counts the issue's runs in order, leaving out quick create", () => {
    const tasks = [
      { id: "c", created_at: "2026-09-03T00:00:00Z", kind: "comment" as const },
      { id: "q", created_at: "2026-09-01T00:00:00Z", kind: "quick_create" as const },
      { id: "a", created_at: "2026-09-02T00:00:00Z", kind: "direct" as const },
    ];
    expect(runNumber(tasks, "a")).toBe(1);
    expect(runNumber(tasks, "c")).toBe(2);
    expect(runNumber(tasks, "missing")).toBe(0);
  });
});

function change(over: Partial<TaskCodeChange>): TaskCodeChange {
  return {
    id: "id", issue_id: "i", task_id: "t", agent_id: "ag", scope: "run", source: "local_worktree",
    repo_key: "repo", repo_label: "acme/app", repo_url: "", branch: "agent/l/mul-1", base_ref: "",
    base_commit: "aaaaaaa", head_commit: "bbbbbbb", file_count: 1, additions: 1, deletions: 0,
    files_truncated: false, patch_available: true, patch_size: 1, patch_omitted: null,
    created_at: "2026-09-01T00:00:00Z",
    ...over,
  };
}

describe("latestLineOfWork", () => {
  it("takes the newest run's branch row, else its run row", () => {
    const changes = [
      change({ id: "r1", task_id: "t1", created_at: "2026-09-01T00:00:00Z" }),
      change({ id: "r2", task_id: "t2", created_at: "2026-09-02T00:00:00Z" }),
      change({ id: "b2", task_id: "t2", scope: "branch", created_at: "2026-09-02T00:00:00Z" }),
      change({ id: "other", task_id: "t3", repo_key: "other", created_at: "2026-09-03T00:00:00Z" }),
    ];
    expect(latestLineOfWork(changes, "repo")?.id).toBe("b2");
    expect(latestLineOfWork(changes.slice(0, 1), "repo")?.id).toBe("r1");
    expect(latestLineOfWork(changes, "missing")).toBeNull();
  });
});

describe("pullRequestForChange", () => {
  const pr = (over: Partial<GitHubPullRequest>): GitHubPullRequest => ({
    id: "pr", workspace_id: "w", repo_owner: "acme", repo_name: "app", number: 1, title: "t",
    state: "open", html_url: "", branch: "feature", author_login: null, author_avatar_url: null,
    merged_at: null, closed_at: null, pr_created_at: "", pr_updated_at: "",
    ...over,
  } as GitHubPullRequest);

  it("matches the PR whose head is the run's branch", () => {
    const prs = [pr({ id: "a", branch: "other" }), pr({ id: "b", branch: "agent/l/mul-1" })];
    expect(pullRequestForChange(prs, change({}))?.id).toBe("b");
  });

  it("never guesses a PR for a branch no PR carries", () => {
    expect(pullRequestForChange([pr({ id: "a", branch: "someone-else" })], change({}))).toBeNull();
  });

  it("takes the only PR in the same repository for a run that names no branch", () => {
    const detached = change({ branch: "" });
    expect(pullRequestForChange([pr({ id: "a", repo_owner: "Acme" })], detached)?.id).toBe("a");
    expect(pullRequestForChange([pr({ id: "a" }), pr({ id: "b" })], detached)).toBeNull();
  });

  it("does not match a same-named branch in another repository", () => {
    expect(pullRequestForChange([pr({ id: "x", repo_name: "other", branch: "agent/l/mul-1" })], change({}))).toBeNull();
  });

  it("ignores other providers", () => {
    expect(pullRequestForChange([pr({ id: "g", provider: "gitlab", branch: "agent/l/mul-1" })], change({}))).toBeNull();
  });
});
