// @vitest-environment node
import { describe, expect, it } from "vitest";
import { SEAgentFleetSchema, SEInfraHealthSnapshotSchema } from "./schemas";

describe("SE infra health schemas (SE-37641/SE-37663)", () => {
  it("parses a snapshot without the agent_fleet section (older backends)", () => {
    const snap = SEInfraHealthSnapshotSchema.parse({
      generated_at: "2026-09-18T10:00:00Z",
      overall: "ok",
      targets: [],
      nodes: {},
      history: {},
    });
    expect(snap.agent_fleet).toBeUndefined();
  });

  it("tolerates a nullish agent_fleet section", () => {
    const snap = SEInfraHealthSnapshotSchema.parse({
      overall: "ok",
      agent_fleet: null,
    });
    expect(snap.agent_fleet).toBeUndefined();
  });

  it("defaults nullish fleet fields defensively", () => {
    const fleet = SEAgentFleetSchema.parse({
      status: "warn",
      quota_streak_active: true,
      auth_streak_active: null,
      runtimes: null,
      failing_agents: [
        {
          agent_id: "agent-1",
          agent_name: "Atlas (CB-Lead)",
          failed: null,
          quota_failed: null,
          auth_failed: undefined,
          last_error_excerpt: "You've hit your usage limit...",
        },
      ],
    });
    expect(fleet.quota_streak_active).toBe(true);
    expect(fleet.auth_streak_active).toBe(false);
    expect(fleet.runtimes).toEqual([]);
    expect(fleet.failing_agents[0]?.completed).toBe(0);
    expect(fleet.failing_agents[0]?.failed).toBe(0);
    expect(fleet.failing_agents[0]?.quota_failed).toBe(0);
    expect(fleet.failing_agents[0]?.auth_failed).toBe(0);
    expect(fleet.failing_agents[0]?.last_error_excerpt).toBe(
      "You've hit your usage limit...",
    );
  });

  it("rejects wrong-typed fleet counts (parseWithFallback handles the fallback)", () => {
    expect(
      SEAgentFleetSchema.safeParse({
        status: "warn",
        failing_agents: [{ agent_id: "a", agent_name: "x", failed: "338" }],
      }).success,
    ).toBe(false);
  });
});
