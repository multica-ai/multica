import { describe, expect, it } from "vitest";
import type { Artifact } from "@multica/core/types";
import {
  selectIssuePlan,
  parseWorkpadChecklist,
  workpadProgress,
} from "./workpad";

function artifact(over: Partial<Artifact>): Artifact {
  return {
    id: "a",
    workspace_id: "ws",
    project_id: null,
    issue_id: "issue-1",
    folder_id: null,
    origin_issue_id: null,
    kind: "note",
    format: "md",
    title: "t",
    body: "",
    file_url: null,
    file_size_bytes: null,
    metadata: {},
    author_type: "agent",
    author_id: "agent-1",
    requester_user_id: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

describe("selectIssuePlan", () => {
  it("returns null when there is no plan", () => {
    expect(selectIssuePlan([])).toBeNull();
    expect(selectIssuePlan(null)).toBeNull();
    expect(selectIssuePlan([artifact({ kind: "note" }), artifact({ kind: "report" })])).toBeNull();
  });

  it("picks the plan artifact and ignores other kinds", () => {
    const plan = artifact({ id: "p", kind: "plan" });
    expect(selectIssuePlan([artifact({ kind: "note" }), plan])?.id).toBe("p");
  });

  it("picks the most recently updated plan when several exist", () => {
    const older = artifact({ id: "old", kind: "plan", updated_at: "2026-01-01T00:00:00Z" });
    const newer = artifact({ id: "new", kind: "plan", updated_at: "2026-02-01T00:00:00Z" });
    expect(selectIssuePlan([older, newer])?.id).toBe("new");
    expect(selectIssuePlan([newer, older])?.id).toBe("new");
  });
});

describe("parseWorkpadChecklist", () => {
  it("returns [] for empty body", () => {
    expect(parseWorkpadChecklist("")).toEqual([]);
    expect(parseWorkpadChecklist(null)).toEqual([]);
  });

  it("parses checked and unchecked items in order, ignoring prose and headings", () => {
    const body = [
      "# Plan",
      "Some intro prose.",
      "- [ ] Step one",
      "- [x] Step two",
      "* [X] Step three",
      "- not a checklist item",
    ].join("\n");
    expect(parseWorkpadChecklist(body)).toEqual([
      { text: "Step one", done: false },
      { text: "Step two", done: true },
      { text: "Step three", done: true },
    ]);
  });

  it("handles CRLF line endings", () => {
    expect(parseWorkpadChecklist("- [ ] a\r\n- [x] b")).toEqual([
      { text: "a", done: false },
      { text: "b", done: true },
    ]);
  });
});

describe("workpadProgress", () => {
  it("counts done vs total", () => {
    expect(workpadProgress(parseWorkpadChecklist("- [ ] a\n- [x] b\n- [x] c"))).toEqual({
      done: 2,
      total: 3,
    });
  });
});
