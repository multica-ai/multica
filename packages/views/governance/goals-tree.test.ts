// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Goal } from "@multica/core/governance";
import { buildGoalTree, progressPercent } from "./goals-tree";

function goal(id: string, scope: Goal["scope_type"], scopeId: string | null, title = id): Goal {
  return {
    id,
    workspace_id: "ws-1",
    scope_type: scope,
    scope_id: scopeId,
    parent_goal_id: null,
    title,
    why: "",
    period_start: "2026-10-01",
    period_end: "2026-12-31",
    owner_type: "member",
    owner_id: "u-1",
    status: "active",
    confidence: "on_track",
    derived_confidence: "on_track",
    confidence_override: null,
    confidence_note: "",
    confidence_changed_at: "",
    progress: null,
    readings_missing: 0,
    source_proposal_id: null,
    key_results: [],
    created_by_type: "member",
    created_by_id: "u-1",
    created_at: "",
    updated_at: "",
  };
}

describe("buildGoalTree", () => {
  it("puts the company first, then floors, squads and initiatives in the order they are listed", () => {
    const tree = buildGoalTree(
      [
        goal("ini-goal", "initiative", "i-1"),
        goal("squad-goal", "squad", "s-1"),
        goal("floor-goal", "floor", "f-1"),
        goal("company-goal", "company", null),
      ],
      {
        floors: [{ id: "f-1", name: "Research" }],
        squads: [{ id: "s-1", name: "Growth" }],
        initiatives: [{ id: "i-1", identifier: "INI-3", title: "Onboarding" }],
      },
    );
    expect(tree.map((s) => s.scope)).toEqual(["company", "floor", "squad", "initiative"]);
    expect(tree[1]).toMatchObject({ name: "Research" });
    expect(tree[2]?.goals.map((g) => g.id)).toEqual(["squad-goal"]);
    expect(tree[3]).toMatchObject({ name: "INI-3 Onboarding" });
  });

  it("keeps a goal whose scope object is not listed, unnamed, rather than dropping it", () => {
    const tree = buildGoalTree([goal("orphan", "squad", "archived-squad")], { floors: [], squads: [], initiatives: [] });
    expect(tree).toHaveLength(1);
    expect(tree[0]).toMatchObject({ scope: "squad", scopeId: "archived-squad", name: null });
    expect(tree[0]?.goals[0]?.id).toBe("orphan");
  });

  it("leaves out an empty section", () => {
    const tree = buildGoalTree([goal("c", "company", null)], {
      floors: [{ id: "f", name: "Empty floor" }],
      squads: [],
      initiatives: [],
    });
    expect(tree.map((s) => s.scope)).toEqual(["company"]);
  });
});

describe("progressPercent", () => {
  it("rounds a fraction to a whole percent and clamps it", () => {
    expect(progressPercent(0.333)).toBe(33);
    expect(progressPercent(1.4)).toBe(100);
    expect(progressPercent(-0.2)).toBe(0);
  });

  it("is null with no reading, so the view says so instead of showing zero", () => {
    expect(progressPercent(null)).toBeNull();
    expect(progressPercent(Number.NaN)).toBeNull();
  });
});
