import { describe, it, expect } from "vitest";
import type { Agent } from "@multica/core/types";

// Mirrors the filter expression in AssigneePicker's filteredAgents useMemo:
//   .filter((a) => !a.archived_at && (query || !a.internal) && nameMatches)
function pickFilteredAgents(agents: Partial<Agent>[], query: string): Partial<Agent>[] {
  const q = query.trim().toLowerCase();
  return agents.filter(
    (a) =>
      !a.archived_at &&
      (q || !a.internal) &&
      a.name!.toLowerCase().includes(q),
  );
}

function agent(overrides: Partial<Agent>): Partial<Agent> {
  return { id: "id", name: "test", archived_at: null, internal: false, ...overrides };
}

describe("assignee picker internal filter (SLE-53)", () => {
  it("shows non-internal agents in default view", () => {
    const agents = [agent({ name: "Public Agent", internal: false })];
    expect(pickFilteredAgents(agents, "")).toHaveLength(1);
  });

  it("hides internal agents in default view (no query)", () => {
    const agents = [agent({ name: "Worker", internal: true })];
    expect(pickFilteredAgents(agents, "")).toHaveLength(0);
  });

  it("shows internal agents when query is active", () => {
    const agents = [agent({ name: "Worker", internal: true })];
    expect(pickFilteredAgents(agents, "work")).toHaveLength(1);
  });

  it("shows currently-assigned internal agent when searched by name", () => {
    const agents = [
      agent({ name: "Art-Code", internal: true }),
      agent({ name: "Art-Review", internal: true }),
    ];
    expect(pickFilteredAgents(agents, "art")).toHaveLength(2);
  });

  it("still hides internal agents when query matches another agent only", () => {
    const agents = [
      agent({ name: "Public", internal: false }),
      agent({ name: "Background Worker", internal: true }),
    ];
    // query "public" matches the non-internal agent only
    const result = pickFilteredAgents(agents, "public");
    expect(result).toHaveLength(1);
    expect(result[0]!.name).toBe("Public");
  });

  it("does not show archived agents regardless of internal flag", () => {
    const agents = [
      agent({ name: "Archived Public", archived_at: "2024-01-01", internal: false }),
      agent({ name: "Archived Internal", archived_at: "2024-01-01", internal: true }),
    ];
    expect(pickFilteredAgents(agents, "")).toHaveLength(0);
    expect(pickFilteredAgents(agents, "archived")).toHaveLength(0);
  });
});
