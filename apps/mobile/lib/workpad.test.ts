import { describe, expect, it } from "vitest";
import {
  selectPlanArtifact,
  parseWorkpadPhases,
  namedPhases,
  workpadProgress,
  phaseStatus,
  phaseComplete,
  type WorkpadArtifact,
} from "./workpad";

function artifact(over: Partial<WorkpadArtifact>): WorkpadArtifact {
  return {
    id: "a",
    kind: "note",
    title: "t",
    body: "",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

describe("selectPlanArtifact", () => {
  it("returns null with no plan", () => {
    expect(selectPlanArtifact([])).toBeNull();
    expect(selectPlanArtifact(null)).toBeNull();
    expect(selectPlanArtifact([artifact({ kind: "note" })])).toBeNull();
  });

  it("picks the plan and the most recently updated when several exist", () => {
    const older = artifact({ id: "old", kind: "plan", updated_at: "2026-01-01T00:00:00Z" });
    const newer = artifact({ id: "new", kind: "plan", updated_at: "2026-02-01T00:00:00Z" });
    expect(selectPlanArtifact([artifact({ kind: "note" }), older, newer])?.id).toBe("new");
  });
});

describe("parseWorkpadPhases", () => {
  it("returns [] for empty body", () => {
    expect(parseWorkpadPhases("")).toEqual([]);
    expect(parseWorkpadPhases(null)).toEqual([]);
  });

  it("groups steps under headings and drops the title-only heading", () => {
    const body = ["# Plan", "## Phase 1", "- [ ] a", "- [x] b", "## Phase 2", "- [ ] c"].join("\n");
    expect(parseWorkpadPhases(body)).toEqual([
      { title: "Phase 1", items: [{ text: "a", done: false }, { text: "b", done: true }] },
      { title: "Phase 2", items: [{ text: "c", done: false }] },
    ]);
  });

  it("keeps a leading null-title phase for steps before the first heading", () => {
    expect(parseWorkpadPhases("- [ ] setup\n## Phase 1\n- [ ] a")).toEqual([
      { title: null, items: [{ text: "setup", done: false }] },
      { title: "Phase 1", items: [{ text: "a", done: false }] },
    ]);
  });

  it("returns one null-title phase for a flat plan", () => {
    expect(parseWorkpadPhases("- [ ] a\n- [x] b")).toEqual([
      { title: null, items: [{ text: "a", done: false }, { text: "b", done: true }] },
    ]);
  });

  it("handles CRLF endings", () => {
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

describe("workpadProgress", () => {
  it("counts done vs total", () => {
    const phases = parseWorkpadPhases("## A\n- [ ] a\n- [x] b\n## B\n- [x] c");
    expect(workpadProgress(phases.flatMap((p) => p.items))).toEqual({ done: 2, total: 3 });
  });
});

describe("phaseStatus", () => {
  it("maps progress to todo / in_progress / done", () => {
    expect(phaseStatus({ done: 0, total: 3 })).toBe("todo");
    expect(phaseStatus({ done: 1, total: 3 })).toBe("in_progress");
    expect(phaseStatus({ done: 3, total: 3 })).toBe("done");
    expect(phaseStatus({ done: 0, total: 0 })).toBe("todo");
  });
});

describe("phaseComplete", () => {
  it("is true only when every step is done", () => {
    expect(phaseComplete({ done: 2, total: 2 })).toBe(true);
    expect(phaseComplete({ done: 1, total: 2 })).toBe(false);
    expect(phaseComplete({ done: 0, total: 0 })).toBe(false);
  });
});
