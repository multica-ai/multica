import { describe, expect, it } from "vitest";
import type { AgentPresenceDetail } from "@multica/core/agents";
import {
  countAgentStatusFilters,
  matchesAgentStatusFilter,
} from "./agent-status-filter";

function presence(
  availability: AgentPresenceDetail["availability"],
  workload: AgentPresenceDetail["workload"],
): AgentPresenceDetail {
  return {
    availability,
    workload,
    runningCount: workload === "working" ? 1 : 0,
    queuedCount: workload === "queued" ? 1 : 0,
    capacity: 1,
  };
}

describe("agent status filter", () => {
  const presenceMap = new Map<string, AgentPresenceDetail>([
    ["online-working", presence("online", "working")],
    ["online-queued", presence("online", "queued")],
    ["offline-working", presence("offline", "working")],
  ]);

  it("matches availability filters independently from workload", () => {
    expect(
      matchesAgentStatusFilter("online-working", presenceMap, "online"),
    ).toBe(true);
    expect(
      matchesAgentStatusFilter("online-queued", presenceMap, "online"),
    ).toBe(true);
    expect(
      matchesAgentStatusFilter("offline-working", presenceMap, "online"),
    ).toBe(false);
  });

  it("matches only currently running agents for working", () => {
    expect(
      matchesAgentStatusFilter("online-working", presenceMap, "working"),
    ).toBe(true);
    expect(
      matchesAgentStatusFilter("offline-working", presenceMap, "working"),
    ).toBe(true);
    expect(
      matchesAgentStatusFilter("online-queued", presenceMap, "working"),
    ).toBe(false);
  });

  it("counts availability and working against the provided agent scope", () => {
    const counts = countAgentStatusFilters(
      [
        { id: "online-working" },
        { id: "online-queued" },
        { id: "missing-presence" },
      ],
      presenceMap,
    );

    expect(counts).toEqual({
      availability: {
        online: 2,
        unstable: 0,
        offline: 0,
      },
      working: 1,
    });
  });
});
