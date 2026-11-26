import { describe, expect, it } from "vitest";
import type {
  RunFileChange,
  RunFileChanges,
} from "@multica/core/agents/file-changes";
import {
  copyRunFileChangePath,
  groupRunFileChanges,
  rootName,
  type RunFileChangeEntry,
} from "./run-file-diff-groups";

const gitRoot = (
  id: string,
  label: string,
  coverage = "complete",
): RunFileChanges["roots"][number] => ({
  id,
  label,
  kind: "git",
  coverage,
  reason: "",
});

const file = (
  id: string,
  rootId: string,
  path = `${id}.ts`,
): RunFileChange => ({
  id,
  rootId,
  path,
  change: "modified",
  beforeBytes: 1,
  afterBytes: 1,
  beforeMode: "100644",
  afterMode: "100644",
  binary: false,
  bodyStatus: "available",
  reason: "",
});

function page(
  roots: RunFileChanges["roots"],
  files: RunFileChange[],
): RunFileChanges {
  return {
    collectionId: "c",
    status: "ready",
    reason: "",
    scope: "registered_workdir_roots",
    roots,
    files,
    filesTotal: files.length,
    totalsComplete: true,
    nextCursor: null,
  };
}

function entries(
  roots: RunFileChanges["roots"],
  files: RunFileChange[],
): RunFileChangeEntry[] {
  const snapshot = page(roots, files);
  return files.map((item) => ({ file: item, page: snapshot }));
}

describe("groupRunFileChanges", () => {
  it("puts project roots before the workdir sentinel and keeps roots order", () => {
    const roots = [
      gitRoot("wd", "workdir"),
      gitRoot("b", "repo-b"),
      gitRoot("a", "repo-a"),
    ];
    const files = [
      file("w1", "wd"),
      file("b1", "b"),
      file("a1", "a"),
      file("w2", "wd"),
    ];
    const groups = groupRunFileChanges(entries(roots, files), roots);
    expect(groups.map((group) => group.label)).toEqual([
      "repo-b",
      "repo-a",
      "workdir",
    ]);
    expect(groups[2]?.files.map((entry) => entry.file.id)).toEqual([
      "w1",
      "w2",
    ]);
  });

  it("omits empty roots and still renders a single-root header", () => {
    const roots = [gitRoot("a", "repo-a"), gitRoot("b", "repo-b")];
    const groups = groupRunFileChanges(
      entries(roots, [file("a1", "a")]),
      roots,
    );
    expect(groups).toHaveLength(1);
    expect(groups[0]?.displayName).toBe("repo-a");
    expect(groups[0]?.files).toHaveLength(1);
  });

  it("keeps a lone workdir group so the path still has a home", () => {
    const roots = [gitRoot("wd", "workdir")];
    const groups = groupRunFileChanges(
      entries(roots, [file("w1", "wd", "notes.md")]),
      roots,
    );
    expect(groups).toHaveLength(1);
    expect(groups[0]?.label).toBe("workdir");
    expect(groups[0]?.displayName).toBe("workdir");
  });

  it("disambiguates identical labels using the root id", () => {
    const roots = [gitRoot("r1", "repo"), gitRoot("r2", "repo")];
    const groups = groupRunFileChanges(
      entries(roots, [file("f1", "r1"), file("f2", "r2")]),
      roots,
    );
    expect(groups.map((group) => group.displayName)).toEqual([
      "repo (r1)",
      "repo (r2)",
    ]);
  });

  it("merges later-page files into the existing root group", () => {
    const roots = [gitRoot("a", "repo-a"), gitRoot("wd", "workdir")];
    const first = page(roots, [file("a1", "a"), file("w1", "wd")]);
    const second = page(roots, [file("a2", "a")]);
    const groups = groupRunFileChanges(
      [
        { file: first.files[0]!, page: first },
        { file: first.files[1]!, page: first },
        { file: second.files[0]!, page: second },
      ],
      roots,
    );
    expect(groups.map((group) => group.label)).toEqual(["repo-a", "workdir"]);
    expect(groups[0]?.files.map((entry) => entry.file.id)).toEqual([
      "a1",
      "a2",
    ]);
  });

  it("marks a group partial from the originating page's root, not the first page", () => {
    const ready = gitRoot("r2", "repo-two");
    const first = page(
      [gitRoot("r1", "repo-one"), ready],
      [file("f1", "r1")],
    );
    const second = page(
      [{ ...ready, kind: "future" }],
      [file("f2", "r2")],
    );
    const groups = groupRunFileChanges(
      [
        { file: first.files[0]!, page: first },
        { file: second.files[0]!, page: second },
      ],
      first.roots,
    );
    expect(groups[0]?.partial).toBe(false);
    expect(groups[1]?.partial).toBe(true);
  });
});

describe("rootName", () => {
  it("falls back to the id when the root is missing", () => {
    expect(rootName("missing", [])).toBe("missing");
  });
});

describe("copyRunFileChangePath", () => {
  it("prefers an absolute candidate when the run workdir can anchor it", () => {
    expect(
      copyRunFileChangePath("repo-one", "src/a.ts", "/Users/dev/proj"),
    ).toBe("/Users/dev/proj/repo-one/src/a.ts");
  });

  it("falls back to the root-relative path instead of a bare file path", () => {
    expect(copyRunFileChangePath("repo-one", "src/a.ts")).toBe(
      "repo-one/src/a.ts",
    );
  });

  it("does not prefix the workdir sentinel", () => {
    expect(copyRunFileChangePath("workdir", "notes.md")).toBe("notes.md");
    expect(copyRunFileChangePath("workdir", "notes.md", "/run")).toBe(
      "/run/notes.md",
    );
  });
});
