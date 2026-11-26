import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import {
  formatActivity,
  latestSquadHandoffRound,
  squadHandoffNote,
  type ActivityT,
} from "./activity-text";

// Renders the selected key and its params, so assertions read the copy choice.
const keyPath = (prefix: string): unknown =>
  new Proxy({}, {
    get: (_target, key) =>
      key === "__path" ? prefix : keyPath(prefix ? `${prefix}.${String(key)}` : String(key)),
  });
const t = ((select: (root: unknown) => { __path: string }, params?: object) =>
  `${select(keyPath("")).__path}${params ? JSON.stringify(params) : ""}`) as unknown as ActivityT;

const handoff = (details: Record<string, string>, created_at = "2026-11-24T10:00:00Z") =>
  ({
    type: "activity",
    action: "squad_handoff",
    actor_type: "agent",
    actor_id: "agent-a",
    details,
    created_at,
  }) as unknown as TimelineEntry;

const names = (_type: string, id: string) => ({ "agent-b": "Reviewer", "agent-l": "Leader", "user-1": "Ada" })[id] ?? "?";

describe("squad handoff activity", () => {
  it("names who the issue went to", () => {
    expect(formatActivity(handoff({ kind: "member", to_type: "agent", to_id: "agent-b" }), t, "en", names))
      .toBe('activity.squad_handoff_to{"name":"Reviewer"}');
    expect(formatActivity(handoff({ kind: "human", to_type: "member", to_id: "user-1" }), t, "en", names))
      .toBe('activity.squad_handoff_human{"name":"Ada"}');
    expect(formatActivity(handoff({ kind: "leader", to_type: "agent", to_id: "agent-l" }), t, "en", names))
      .toBe('activity.squad_handoff_leader{"name":"Leader"}');
  });

  it("notes the round from the second pass on, and why the leader took over", () => {
    expect(squadHandoffNote(handoff({ kind: "member" }), t)).toBeNull();
    expect(squadHandoffNote(handoff({ kind: "member", round: "2" }), t)).toBe('activity.squad_handoff_round{"round":2}');
    expect(squadHandoffNote(handoff({ kind: "leader" }), t)).toBeNull();
    expect(squadHandoffNote(handoff({ kind: "leader", reason: "round_limit" }), t))
      .toBe('activity.squad_handoff_reason_round_limit{"limit":3}');
    expect(squadHandoffNote(handoff({ kind: "leader", reason: "run_failed" }), t))
      .toBe("activity.squad_handoff_reason_run_failed");
  });

  it("reads the current round off the newest handoff only", () => {
    const second = handoff({ kind: "member", to_type: "agent", to_id: "agent-b", round: "2" }, "2026-11-24T10:00:00Z");
    expect(latestSquadHandoffRound([second])).toEqual({ agentId: "agent-b", round: 2 });
    expect(latestSquadHandoffRound([handoff({ kind: "member", to_type: "agent", to_id: "agent-b" })])).toBeNull();
    const backToLeader = handoff({ kind: "leader", to_type: "agent", to_id: "agent-l" }, "2026-11-24T11:00:00Z");
    expect(latestSquadHandoffRound([backToLeader, second])).toBeNull();
  });
});
