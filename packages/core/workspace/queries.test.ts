import { describe, expect, it } from "vitest";
import { selectSkillAssignments } from "./queries";
import type { Agent } from "../types/agent";

function makeAgent(overrides: Partial<Agent> & { id: string }): Agent {
  // This factory only sets the fields that selectSkillAssignments reads.
  // The full Agent shape has more required fields (status, runtime_id format,
  // etc.) that the function under test doesn't touch, so we cast to keep the
  // test focused on the selector's logic, not on Agent-shape plumbing.
  return {
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: overrides.id,
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "cloud",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [],
    owner_id: null,
    skills: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  } as Agent;
}

function makeSkillSummary(id: string) {
  return { id, name: id, description: "", config: {} };
}

describe("selectSkillAssignments", () => {
  it("returns an empty map when agents is undefined", () => {
    const map = selectSkillAssignments(undefined);
    expect(map.size).toBe(0);
  });

  it("returns an empty map when agents is an empty array", () => {
    const map = selectSkillAssignments([]);
    expect(map.size).toBe(0);
  });

  it("returns an empty map when agents have no skills", () => {
    const map = selectSkillAssignments([
      makeAgent({ id: "a1", skills: [] }),
      makeAgent({ id: "a2", skills: [] }),
    ]);
    expect(map.size).toBe(0);
  });

  it("maps a single skill to its bound agents", () => {
    const map = selectSkillAssignments([
      makeAgent({ id: "a1", skills: [makeSkillSummary("s1")] }),
      makeAgent({ id: "a2", skills: [makeSkillSummary("s1")] }),
    ]);
    expect(map.size).toBe(1);
    expect(map.get("s1")?.map((a) => a.id)).toEqual(["a1", "a2"]);
  });

  it("groups multiple distinct skills independently", () => {
    const map = selectSkillAssignments([
      makeAgent({
        id: "a1",
        skills: [makeSkillSummary("s1"), makeSkillSummary("s2")],
      }),
      makeAgent({ id: "a2", skills: [makeSkillSummary("s2")] }),
    ]);
    expect(map.size).toBe(2);
    expect(map.get("s1")?.map((a) => a.id)).toEqual(["a1"]);
    expect(map.get("s2")?.map((a) => a.id)).toEqual(["a1", "a2"]);
  });

  it("skips archived agents", () => {
    const map = selectSkillAssignments([
      makeAgent({
        id: "a1",
        archived_at: "2026-06-01T00:00:00Z",
        skills: [makeSkillSummary("s1")],
      }),
      makeAgent({ id: "a2", skills: [makeSkillSummary("s1")] }),
    ]);
    expect(map.size).toBe(1);
    // a1 is skipped because it is archived.
    expect(map.get("s1")?.map((a) => a.id)).toEqual(["a2"]);
  });

  it("does not mutate the input agents array", () => {
    const a1 = makeAgent({ id: "a1", skills: [makeSkillSummary("s1")] });
    const input: Agent[] = [a1];
    const snapshot = JSON.stringify(input);
    selectSkillAssignments(input);
    expect(JSON.stringify(input)).toBe(snapshot);
  });
});
