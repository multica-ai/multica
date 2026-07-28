import { describe, expect, it } from "vitest";
import type { Artifact } from "@multica/core/types";
import {
  selectIssuePlan,
  parseWorkpadChecklist,
  workpadProgress,
  parseWorkpadPhases,
  namedPhases,
  phaseStatus,
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

describe("parseWorkpadPhases", () => {
  it("returns [] for empty body", () => {
    expect(parseWorkpadPhases("")).toEqual([]);
    expect(parseWorkpadPhases(null)).toEqual([]);
  });

  it("groups steps under their headings and drops the title-only heading", () => {
    const body = [
      "# Plan",
      "## Fase 1: Byg",
      "- [ ] a",
      "- [x] b",
      "## Fase 2: Test",
      "- [ ] c",
    ].join("\n");
    expect(parseWorkpadPhases(body)).toEqual([
      { title: "Fase 1: Byg", items: [{ text: "a", done: false }, { text: "b", done: true }] },
      { title: "Fase 2: Test", items: [{ text: "c", done: false }] },
    ]);
  });

  it("keeps steps before the first heading as a leading null-title phase", () => {
    const body = ["- [ ] setup", "## Fase 1", "- [ ] a"].join("\n");
    expect(parseWorkpadPhases(body)).toEqual([
      { title: null, items: [{ text: "setup", done: false }] },
      { title: "Fase 1", items: [{ text: "a", done: false }] },
    ]);
  });

  it("returns one null-title phase for a flat plan with no headings", () => {
    const phases = parseWorkpadPhases("- [ ] a\n- [x] b");
    expect(phases).toEqual([
      { title: null, items: [{ text: "a", done: false }, { text: "b", done: true }] },
    ]);
  });

  it("preserves flat step order (flatMap equals parseWorkpadChecklist)", () => {
    const body = "## One\n- [ ] a\n## Two\n- [x] b\n- [ ] c";
    const flat = parseWorkpadPhases(body).flatMap((p) => p.items);
    expect(flat).toEqual(parseWorkpadChecklist(body));
  });

  it("handles headings of any level and CRLF endings", () => {
    expect(parseWorkpadPhases("### Deep\r\n- [ ] a")).toEqual([
      { title: "Deep", items: [{ text: "a", done: false }] },
    ]);
  });
});

describe("namedPhases", () => {
  it("keeps only titled phases", () => {
    const phases = parseWorkpadPhases("- [ ] pre\n## A\n- [ ] a\n## B\n- [ ] b");
    expect(namedPhases(phases).map((p) => p.title)).toEqual(["A", "B"]);
  });
});

describe("phaseStatus", () => {
  it("is todo when nothing is done", () => {
    expect(phaseStatus({ done: 0, total: 3 })).toBe("todo");
  });
  it("is in_progress when some but not all are done", () => {
    expect(phaseStatus({ done: 1, total: 3 })).toBe("in_progress");
  });
  it("is done when every step is done", () => {
    expect(phaseStatus({ done: 3, total: 3 })).toBe("done");
  });
  it("treats an empty phase as todo", () => {
    expect(phaseStatus({ done: 0, total: 0 })).toBe("todo");
  });
});
