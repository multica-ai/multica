// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { EngineeringConfig, EngineeringStage, IssueGraphNode } from "@multica/core/api";
import type { Agent, Member, Project, TimelineEntry } from "@multica/core/types";
import {
  buildEngineeringRoadmap,
  engineeringPermissions,
  selectEngineeringComment,
  strixnovaSkillBindings,
} from "./engineering-utils";

const comment = (
  id: string,
  content: string,
  createdAt: string,
  options: { parentId?: string | null; resolvedAt?: string | null } = {},
): TimelineEntry => ({
  id,
  type: "comment",
  actor_type: "member",
  actor_id: "user-1",
  created_at: createdAt,
  content,
  parent_id: options.parentId ?? null,
  resolved_at: options.resolvedAt ?? null,
});
const config = (deciderMemberIds: string[] = []) => ({ deciderMemberIds }) as EngineeringConfig;
const members = [
  { user_id: "owner-1", role: "owner" },
  { user_id: "member-1", role: "member" },
  { user_id: "lead-1", role: "member" },
] as Member[];
const node = (
  id: string,
  stage: number | null,
  status = "todo",
  statusCategory = "unstarted",
  parentIssueId: string | null = null,
) => ({
  id,
  identifier: `OL-${id}`,
  title: `Task ${id}`,
  stage,
  status,
  statusCategory,
  parentIssueId,
}) as IssueGraphNode;

describe("selectEngineeringComment", () => {
  it("prefers an unresolved HTML decision card over every other format", () => {
    const entries = [
      comment("html", "preface\n```html title=\"决策卡：交付方案\"\ncontent", "2026-01-01T00:00:00Z"),
      comment("markdown", "```markdown title=\"决策卡：交付方案\"\ncontent", "2026-01-03T00:00:00Z"),
      comment("plain", "Anything can be the first line", "2026-01-04T00:00:00Z"),
      comment("reply", "```html title=\"决策卡：回复\"\ncontent", "2026-01-05T00:00:00Z", { parentId: "html" }),
    ];
    expect(selectEngineeringComment(entries)?.id).toBe("html");
  });

  it("uses Markdown decision cards as the next structured fallback", () => {
    const entries = [
      comment("old", "plain", "2026-01-01T00:00:00Z"),
      comment("card", "任意首行\n```markdown title=\"决策卡 v2\"\ncontent", "2026-01-02T00:00:00Z"),
      comment("new", "newest unresolved", "2026-01-03T00:00:00Z"),
    ];
    expect(selectEngineeringComment(entries)?.id).toBe("card");
  });

  it("supports structured decider mentions without inspecting a fixed first line", () => {
    const entries = [
      comment("mention", "Arbitrary prose\n[@Master](mention://member/decider-1)", "2026-01-01T00:00:00Z"),
      comment("new", "newest unresolved", "2026-01-03T00:00:00Z"),
    ];
    expect(selectEngineeringComment(entries, ["decider-1"])?.id).toBe("mention");
  });

  it("falls back to the newest unresolved root, then the newest root", () => {
    const unresolved = [
      comment("old", "plain", "2026-01-01T00:00:00Z"),
      comment("new", "latest root", "2026-01-03T00:00:00Z"),
      comment("reply", "latest reply", "2026-01-04T00:00:00Z", { parentId: "new" }),
    ];
    expect(selectEngineeringComment(unresolved)?.id).toBe("new");
    const resolved = [
      comment("old", "plain", "2026-01-01T00:00:00Z", { resolvedAt: "2026-01-02T00:00:00Z" }),
      comment("resolved-card", "```html title=\"决策卡\"\ncontent", "2026-01-02T00:00:00Z", { resolvedAt: "2026-01-03T00:00:00Z" }),
      comment("resolved-new", "resolved root", "2026-01-04T00:00:00Z", { resolvedAt: "2026-01-05T00:00:00Z" }),
    ];
    expect(selectEngineeringComment(resolved)?.id).toBe("resolved-new");
    expect(selectEngineeringComment([])).toBeNull();
  });
});

describe("engineeringPermissions", () => {
  it("includes admins and the human project lead by default", () => {
    const project = { lead_type: "member", lead_id: "lead-1" } as Project;
    expect(engineeringPermissions("lead-1", members, project, config())).toEqual({ isAdmin: false, isDecider: true, deciderIds: ["lead-1", "owner-1"] });
    expect(engineeringPermissions("member-1", members, project, config()).isDecider).toBe(false);
  });

  it("uses the explicit decider list while retaining admins", () => {
    const result = engineeringPermissions("member-1", members, undefined, config(["member-1"]));
    expect(result).toEqual({ isAdmin: false, isDecider: true, deciderIds: ["member-1", "owner-1"] });
    expect(engineeringPermissions("lead-1", members, undefined, config(["member-1"])).isDecider).toBe(false);
  });
});

describe("buildEngineeringRoadmap", () => {
  it("supports dynamic stages beyond five and keeps every task", () => {
    const tasks = Array.from({ length: 7 }, (_, index) => node(String(index + 1), 7));
    const configured: EngineeringStage[] = [
      { stage: 2, title: "Foundation", goal: "Contracts" },
      { stage: 7, title: "Frontend", goal: "All views" },
    ];
    const roadmap = buildEngineeringRoadmap([...tasks, node("outside", null)], configured, 7);
    expect(roadmap.stages.map((stage) => stage.stage)).toEqual([2, 7]);
    expect(roadmap.stages[1]).toMatchObject({ title: "Frontend", total: 7, todo: 7, status: "current" });
    expect(roadmap.stages[1]?.tasks).toHaveLength(7);
    expect(roadmap.outside.map((task) => task.id)).toEqual(["outside"]);
  });

  it("includes sparse task and current stages and derives gate states", () => {
    const roadmap = buildEngineeringRoadmap([
      node("done-1", 1, "done", "done"),
      node("review-1", 6, "in_review", "started"),
      node("blocked-6", 6, "blocked", "started"),
    ], [], 6);
    expect(roadmap.stages.map((stage) => [stage.stage, stage.status])).toEqual([[1, "completed"], [6, "current"]]);
    expect(roadmap.stages[1]).toMatchObject({ blocked: 1, inReview: 1, total: 2 });
    const review = buildEngineeringRoadmap([node("done-6", 6, "done", "done")], [], 6);
    expect(review.stages[0]?.status).toBe("review");
    const emptyCurrent = buildEngineeringRoadmap([], [{ stage: 6, title: "Empty", goal: "" }], 6);
    expect(emptyCurrent.stages[0]?.status).toBe("current");
  });

  it("drops historical children of closed parents without mixing them into outside work", () => {
    const roadmap = buildEngineeringRoadmap([
      node("closed-parent", null, "done", "done"),
      node("historical", 3, "todo", "unstarted", "closed-parent"),
      node("cancelled-parent", null, "cancelled", "closed"),
      node("cancelled-history", 4, "todo", "unstarted", "cancelled-parent"),
      node("active-parent", null, "in_progress", "started"),
      node("active-child", 8, "todo", "unstarted", "active-parent"),
      node("closed-outside", null, "done", "done"),
      node("open-outside", null),
    ], [], 1);
    expect(roadmap.stages.flatMap((stage) => stage.tasks).map((task) => task.id)).toEqual(["active-child"]);
    expect(roadmap.outside.map((task) => task.id)).toEqual(["active-parent", "open-outside"]);
  });
});

describe("strixnovaSkillBindings", () => {
  it("lists only active agents and enabled core engineering skills", () => {
    const agents = [
      {
        id: "agent-1",
        name: "Frontend",
        archived_at: null,
        skills: [
          { name: "strixnova-deck", enabled: true },
          { name: "strixnova-discovery" },
          { name: "other-skill", enabled: true },
        ],
      },
      {
        id: "agent-2",
        name: "Archived",
        archived_at: "2026-01-01T00:00:00Z",
        skills: [{ name: "strixnova-deck" }],
      },
      {
        id: "agent-3",
        name: "Disabled",
        archived_at: null,
        skills: [{ name: "strixnova-tdd-and-debugging", enabled: false }],
      },
    ] as Agent[];
    expect(strixnovaSkillBindings(agents)).toEqual([{
      agent: agents[0],
      skills: ["strixnova-discovery", "strixnova-deck"],
    }]);
  });
});
