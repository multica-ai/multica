// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue, Squad } from "@multica/core/types";
import { focusIssues, leadersNeedingYou, officeTagOf } from "./office-tag";

function issue(id: string, status: string, assignee: { type: "agent" | "squad"; id: string } | null): Issue {
  return { id, status, assignee_type: assignee?.type ?? null, assignee_id: assignee?.id ?? null } as Issue;
}

const SQUADS = [{ id: "sq", leader_id: "lead" }] as Squad[];
const ALL = ["todo", "in_progress", "in_review", "done"];

describe("focusIssues", () => {
  it("prefers the issue in progress, then the first open one", () => {
    const map = focusIssues(
      [issue("a", "todo", { type: "agent", id: "x" }), issue("b", "in_progress", { type: "agent", id: "x" })],
      ALL,
      SQUADS,
    );
    expect(map.get("x")?.id).toBe("b");
  });

  it("counts a squad's issue for its leader", () => {
    const map = focusIssues([issue("a", "in_review", { type: "squad", id: "sq" })], ALL, SQUADS);
    expect(map.get("lead")?.id).toBe("a");
  });

  it("skips done issues, hidden statuses, unassigned issues and unknown squads", () => {
    const map = focusIssues(
      [
        issue("done", "done", { type: "agent", id: "x" }),
        issue("hidden", "blocked", { type: "agent", id: "x" }),
        issue("none", "todo", null),
        issue("ghost", "todo", { type: "squad", id: "nope" }),
      ],
      ALL,
      SQUADS,
    );
    expect(map.size).toBe(0);
  });
});

describe("officeTagOf", () => {
  const focus = new Map([["x", { status: "todo" }]]);

  it("says Needs you before anything else", () => {
    expect(officeTagOf("x", new Set(["x"]), focus)).toEqual({ kind: "needs_you" });
  });

  it("falls back to the focus issue's status, then to idle", () => {
    expect(officeTagOf("x", new Set(), focus)).toEqual({ kind: "status", status: "todo" });
    expect(officeTagOf("y", new Set(), focus)).toEqual({ kind: "idle" });
  });
});

describe("leadersNeedingYou", () => {
  it("names the leader of each squad with a pending proposal", () => {
    expect(leadersNeedingYou(SQUADS, new Set(["sq"]))).toEqual(new Set(["lead"]));
    expect(leadersNeedingYou(SQUADS, new Set())).toEqual(new Set());
  });
});
