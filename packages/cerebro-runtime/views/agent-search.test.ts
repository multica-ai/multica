import { describe, expect, it } from "vitest";
import { matchesAgentSearch, type AgentSearchFields } from "./agent-search";

function makeFields(overrides: Partial<AgentSearchFields> = {}): AgentSearchFields {
  return {
    name: "Josephine - Developer",
    description: "Frontend specialist",
    model: "claude-opus-4-7",
    thinkingLevel: "high",
    runtimeName: "hvejsel-macbook-24.local",
    ownerName: "Jesper Hvejsel",
    accountLabel: "jesperhvejsel@gmail.com CLAUDE",
    ...overrides,
  };
}

describe("matchesAgentSearch", () => {
  it("matches everything on an empty query", () => {
    expect(matchesAgentSearch(makeFields(), "")).toBe(true);
    expect(matchesAgentSearch(makeFields(), "   ")).toBe(true);
  });

  it("matches the name, model, runtime and owner (case-insensitive)", () => {
    expect(matchesAgentSearch(makeFields(), "josephine")).toBe(true);
    expect(matchesAgentSearch(makeFields(), "OPUS")).toBe(true);
    expect(matchesAgentSearch(makeFields(), "macbook-24")).toBe(true);
    expect(matchesAgentSearch(makeFields(), "hvejsel")).toBe(true);
  });

  // FIR-2669 follow-up: the actual bug — searching by the account email.
  it("matches the account email + provider", () => {
    expect(matchesAgentSearch(makeFields(), "jesperhvejsel@gmail.com")).toBe(true);
    expect(matchesAgentSearch(makeFields(), "gmail")).toBe(true);
    expect(matchesAgentSearch(makeFields(), "codex")).toBe(false);
  });

  it("does not throw when optional fields are missing", () => {
    const fields: AgentSearchFields = { name: "Bare" };
    expect(matchesAgentSearch(fields, "bare")).toBe(true);
    expect(matchesAgentSearch(fields, "gmail")).toBe(false);
  });
});
