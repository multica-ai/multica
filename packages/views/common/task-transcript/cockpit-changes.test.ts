import { describe, expect, it } from "vitest";
import type { TimelineItem } from "./build-timeline";
import { summarizeCockpitFileChanges } from "./cockpit-changes";

describe("summarizeCockpitFileChanges", () => {
  it("collects replacement edits and ignores non-file tools", () => {
    const items: TimelineItem[] = [
      {
        seq: 1,
        type: "tool_use",
        tool: "exec_command",
        input: { command: "pnpm test" },
      },
      {
        seq: 2,
        type: "tool_use",
        tool: "Edit",
        call_id: "edit-1",
        input: {
          file_path: "src/quantizer.py",
          old_string: "scale = 1\nreturn scale",
          new_string: "scale = calibrate(x)\nvalidate(scale)\nreturn scale",
        },
      },
      {
        seq: 3,
        type: "tool_result",
        tool: "Edit",
        call_id: "edit-1",
        output: "completed",
      },
    ];

    expect(summarizeCockpitFileChanges(items)).toEqual([
      {
        path: "src/quantizer.py",
        changeKind: "update",
        additions: 2,
        deletions: 1,
        hasLineCounts: true,
        status: "applied",
        lastSeq: 3,
      },
    ]);
  });

  it("merges repeated patch activity and keeps the most recent file first", () => {
    const items: TimelineItem[] = [
      {
        seq: 3,
        type: "tool_use",
        tool: "patch_apply",
        call_id: "patch-1",
        input: {
          changes: [
            {
              path: "src/a.ts",
              kind: "update",
              diff: "@@ -1 +1,2 @@\n-old\n+new\n+next\n",
            },
            {
              path: "src/b.ts",
              kind: "add",
              content: "one\ntwo",
            },
          ],
        },
      },
      {
        seq: 4,
        type: "tool_result",
        tool: "patch_apply",
        call_id: "patch-1",
        output: "completed (2 files)",
      },
      {
        seq: 8,
        type: "tool_use",
        tool: "patch_apply",
        call_id: "patch-2",
        input: {
          changes: [
            {
              path: "src/a.ts",
              kind: "update",
              diff: "@@ -4 +4 @@\n-before\n+after\n",
            },
          ],
        },
      },
      {
        seq: 9,
        type: "tool_result",
        tool: "patch_apply",
        call_id: "patch-2",
        output: "completed (1 file)",
      },
    ];

    expect(summarizeCockpitFileChanges(items)).toEqual([
      {
        path: "src/a.ts",
        changeKind: "update",
        additions: 3,
        deletions: 2,
        hasLineCounts: true,
        status: "applied",
        lastSeq: 9,
      },
      {
        path: "src/b.ts",
        changeKind: "add",
        additions: 2,
        deletions: 0,
        hasLineCounts: true,
        status: "applied",
        lastSeq: 4,
      },
    ]);
  });

  it("marks whole-file writes as changed without inventing a diff count", () => {
    const items: TimelineItem[] = [{
      seq: 4,
      type: "tool_use",
      tool: "write_file",
      call_id: "write-1",
      input: { path: "src/generated.ts", content: "export const value = 1;" },
    }, {
      seq: 5,
      type: "tool_result",
      tool: "write_file",
      call_id: "write-1",
      output: "completed",
    }];

    expect(summarizeCockpitFileChanges(items)).toEqual([
      {
        path: "src/generated.ts",
        changeKind: "update",
        additions: 0,
        deletions: 0,
        hasLineCounts: false,
        status: "applied",
        lastSeq: 5,
      },
    ]);
  });

  it("matches concurrent edit results by call id and does not count failed edits as applied", () => {
    const items = [
      {
        seq: 10,
        type: "tool_use",
        tool: "patch_apply",
        call_id: "edit-a",
        input: {
          changes: [{ path: "src/a.ts", kind: "update", diff: "@@ -1 +1 @@\n-old\n+new\n" }],
        },
      },
      {
        seq: 11,
        type: "tool_use",
        tool: "patch_apply",
        call_id: "edit-b",
        input: {
          changes: [{ path: "src/b.ts", kind: "update", diff: "@@ -1 +1 @@\n-old\n+bad\n" }],
        },
      },
      {
        seq: 12,
        type: "tool_result",
        tool: "patch_apply",
        call_id: "edit-b",
        output: "failed (1 file)",
      },
      {
        seq: 13,
        type: "tool_result",
        tool: "patch_apply",
        call_id: "edit-a",
        output: "completed (1 file)",
      },
    ] as TimelineItem[];

    expect(summarizeCockpitFileChanges(items)).toEqual([
      {
        path: "src/a.ts",
        changeKind: "update",
        additions: 1,
        deletions: 1,
        hasLineCounts: true,
        status: "applied",
        lastSeq: 13,
      },
      {
        path: "src/b.ts",
        changeKind: "update",
        additions: 0,
        deletions: 0,
        hasLineCounts: false,
        status: "failed",
        lastSeq: 12,
      },
    ]);
  });

  it("falls back only to an untagged legacy edit during a rolling upgrade", () => {
    const items = [
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        input: {
          file_path: "src/legacy.ts",
          old_string: "before",
          new_string: "after",
        },
      },
      {
        seq: 2,
        type: "tool_result",
        tool: "Edit",
        call_id: "new-daemon-result",
        output: "completed",
      },
    ] as TimelineItem[];

    expect(summarizeCockpitFileChanges(items)).toEqual([
      {
        path: "src/legacy.ts",
        changeKind: "update",
        additions: 1,
        deletions: 1,
        hasLineCounts: true,
        status: "applied",
        lastSeq: 2,
      },
    ]);
  });

  it("leaves tagged concurrent edits pending when an untagged result is ambiguous", () => {
    const items = [
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        call_id: "edit-a",
        input: { file_path: "src/a.ts", old_string: "a", new_string: "A" },
      },
      {
        seq: 2,
        type: "tool_use",
        tool: "Edit",
        call_id: "edit-b",
        input: { file_path: "src/b.ts", old_string: "b", new_string: "B" },
      },
      {
        seq: 3,
        type: "tool_result",
        tool: "Edit",
        output: "completed",
      },
    ] as TimelineItem[];

    expect(summarizeCockpitFileChanges(items)).toEqual([
      {
        path: "src/b.ts",
        changeKind: "update",
        additions: 0,
        deletions: 0,
        hasLineCounts: false,
        status: "pending",
        lastSeq: 2,
      },
      {
        path: "src/a.ts",
        changeKind: "update",
        additions: 0,
        deletions: 0,
        hasLineCounts: false,
        status: "pending",
        lastSeq: 1,
      },
    ]);
  });
});
