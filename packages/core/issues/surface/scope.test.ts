import { describe, expect, it } from "vitest";
import {
  actorKindForViewVariant,
  assigneeTypesForActorKind,
  myRelationForViewVariant,
  UnsupportedIssueScopeError,
  issueScopeKey,
} from "./scope";
import { buildIssueSurfaceQueryPlan } from "./query-plan";

describe("issue surface scope", () => {
  it("builds stable surface keys", () => {
    expect(issueScopeKey({ type: "workspace" })).toBe("workspace:all");
    expect(issueScopeKey({ type: "workspace", actorKind: "agents" })).toBe(
      "workspace:agents",
    );
    expect(
      issueScopeKey({ type: "my", relation: "assigned", userId: "u1" }),
    ).toBe("my:u1:assigned");
    expect(issueScopeKey({ type: "project", projectId: "p1" })).toBe(
      "project:p1",
    );
    expect(
      issueScopeKey({
        type: "actor",
        actorType: "agent",
        actorId: "a1",
        relation: "created",
      }),
    ).toBe("actor:agent:a1:created");
    expect(issueScopeKey({ type: "team", teamId: "t1" })).toBe("team:t1");
  });

  it("compiles each scope to its key, gantt filter, and create defaults", () => {
    expect(buildIssueSurfaceQueryPlan({ type: "workspace" })).toEqual({
      scopeKey: "workspace:all",
      queryFilter: {},
      createDefaults: {},
    });
    expect(
      buildIssueSurfaceQueryPlan({ type: "workspace", actorKind: "agents" }),
    ).toEqual({
      scopeKey: "workspace:agents",
      queryFilter: { assignee_types: ["agent", "squad"] },
      createDefaults: {},
    });
    expect(
      buildIssueSurfaceQueryPlan({
        type: "project",
        projectId: "p1",
        actorKind: "members",
      }),
    ).toEqual({
      scopeKey: "project:p1:members",
      queryFilter: { project_id: "p1", assignee_types: ["member"] },
      createDefaults: { project_id: "p1" },
    });
    expect(
      buildIssueSurfaceQueryPlan({
        type: "my",
        relation: "assigned",
        userId: "u1",
      }),
    ).toEqual({
      scopeKey: "my:u1:assigned",
      queryFilter: { assignee_id: "u1" },
      createDefaults: { assignee_type: "member", assignee_id: "u1" },
    });
    expect(
      buildIssueSurfaceQueryPlan({
        type: "actor",
        actorType: "agent",
        actorId: "a1",
        relation: "assigned",
      }),
    ).toEqual({
      scopeKey: "actor:agent:a1:assigned",
      queryFilter: { assignee_id: "a1" },
      createDefaults: { assignee_type: "agent", assignee_id: "a1" },
    });
  });

  it("throws for team until the issue API has a team filter", () => {
    const scope = { type: "team" as const, teamId: "t1" };
    expect(() => buildIssueSurfaceQueryPlan(scope)).toThrow(
      UnsupportedIssueScopeError,
    );
  });
});

describe("assigneeTypesForActorKind", () => {
  it("maps the three tabs to their API values", () => {
    expect(assigneeTypesForActorKind("members")).toEqual(["member"]);
    expect(assigneeTypesForActorKind("agents")).toEqual(["agent", "squad"]);
    expect(assigneeTypesForActorKind("all")).toBeUndefined();
    expect(assigneeTypesForActorKind(undefined)).toBeUndefined();
  });
});

describe("actorKindForViewVariant", () => {
  it("resolves saved-view variants to a tab, defaulting to all", () => {
    expect(actorKindForViewVariant("members")).toBe("members");
    expect(actorKindForViewVariant("agents")).toBe("agents");
    expect(actorKindForViewVariant(null)).toBe("all");
    expect(actorKindForViewVariant(undefined)).toBe("all");
    expect(actorKindForViewVariant("involved")).toBe("all");
  });
});

describe("myRelationForViewVariant", () => {
  it("resolves my-view variants to a relation, defaulting to all", () => {
    expect(myRelationForViewVariant("assigned")).toBe("assigned");
    expect(myRelationForViewVariant("created")).toBe("created");
    expect(myRelationForViewVariant("involved")).toBe("involved");
    expect(myRelationForViewVariant("any")).toBe("all");
    expect(myRelationForViewVariant(null)).toBe("all");
    expect(myRelationForViewVariant("members")).toBe("all");
  });
});
