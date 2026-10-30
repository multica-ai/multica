// @vitest-environment node
import { setSchemaLogger } from "../api/schema";
import { noopLogger } from "../logger";
import { describe, it, expect, vi } from "vitest";
import {
  parseCodeChanges,
  parseDiffLines,
  codeChangeKeys,
} from "./code-changes";

describe("code changes wire boundary", () => {
  it("requires the saved source and maps snapshot/history fields", () => {
    expect(() => parseCodeChanges({ available: true, runs: [] })).toThrow();
    const result = parseCodeChanges({
      available: true,
      runs: [
        {
          task_id: "run-a",
          status: "completed",
          created_at: "2026-10-25T00:00:00Z",
          file_count: 1,
        },
      ],
      source_task_id: "run-a",
      snapshot: {
        version: 1,
        base_ref: "a",
        head_ref: "b",
        files: [],
        truncated: false,
        working_tree_dirty: true,
      },
    });
    expect(result.sourceTaskId).toBe("run-a");
    expect(result.runs[0]?.fileCount).toBe(1);
    expect(result.snapshot?.workingTreeDirty).toBe(true);
    expect(codeChangeKeys.detail("ws-a", "issue", "run")).not.toEqual(
      codeChangeKeys.detail("ws-b", "issue", "run"),
    );
  });
  it("keeps old and new line numbers across separate hunks", () => {
    const lines = parseDiffLines(
      "--- a/file\n+++ b/file\n@@ -2,2 +4,2 @@\n context\n-old\n+new\n\\ No newline at end of file\n@@ -8 +10 @@\n-later\n+latest",
    );
    expect(lines[0]?.oldLine).toBeUndefined();
    expect(lines[3]).toMatchObject({ kind: "context", oldLine: 2, newLine: 4 });
    expect(lines[4]).toMatchObject({ kind: "remove", oldLine: 3 });
    expect(lines[5]).toMatchObject({ kind: "add", newLine: 5 });
    expect(lines[8]).toMatchObject({ kind: "remove", oldLine: 8 });
    expect(lines[9]).toMatchObject({ kind: "add", newLine: 10 });
  });
});

it("does not log private diff bodies when the wire response is malformed", () => {
  const warn = vi.fn();
  setSchemaLogger({ debug: vi.fn(), info: vi.fn(), warn, error: vi.fn() });
  try {
    expect(() =>
      parseCodeChanges({
        available: true,
        runs: [],
        snapshot: { private: "never-log-this-source" },
      }),
    ).toThrow();
    expect(warn).toHaveBeenCalled();
    expect(JSON.stringify(warn.mock.calls)).not.toContain(
      "never-log-this-source",
    );
  } finally {
    setSchemaLogger(noopLogger);
  }
});
