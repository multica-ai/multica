import { describe, it, expect } from "vitest";
import { snapshotToFields, changedSnapshotKeys } from "./snapshot-fields";
import type { AgentContextSnapshot } from "@multica/core/types";

const base = {
  skill_ids: ["11111111-1111-1111-1111-111111111111", "unknown-id"],
} as unknown as AgentContextSnapshot;

describe("snapshotToFields — skills", () => {
  it("lists raw skill ids when no resolver is supplied", () => {
    const skills = snapshotToFields(base).find((f) => f.key === "skill_ids");
    expect(skills?.value).toBe(
      "11111111-1111-1111-1111-111111111111\nunknown-id",
    );
    // Raw ids stay monospace.
    expect(skills?.mono).toBe(true);
  });

  it("resolves ids to human-readable names and passes unknown ids through", () => {
    const resolveSkill = (id: string) =>
      id === "11111111-1111-1111-1111-111111111111" ? "plain-dansk-gate" : id;
    const skills = snapshotToFields(base, { resolveSkill }).find(
      (f) => f.key === "skill_ids",
    );
    expect(skills?.value).toBe("plain-dansk-gate\nunknown-id");
    // Names read as prose, not code.
    expect(skills?.mono).toBe(false);
  });
});

// FIR-3212 Approval slice — the approval panel asks the server what each changed
// field MEANS on this agent's engine, so it has to name the same fields the diff
// beside it renders. One predicate, used by both, or the two disagree about what
// the proposal touches.
describe("changedSnapshotKeys", () => {
  const from = {
    instructions: "old",
    model: "claude-opus-4-8",
    skill_ids: ["a"],
  } as unknown as AgentContextSnapshot;

  it("names only the fields whose normalised value differs", () => {
    const to = {
      instructions: "new",
      model: "claude-opus-4-8",
      skill_ids: ["a"],
    } as unknown as AgentContextSnapshot;
    expect(changedSnapshotKeys(from, to)).toEqual(["instructions"]);
  });

  it("returns nothing for identical snapshots", () => {
    expect(changedSnapshotKeys(from, from)).toEqual([]);
  });

  // Whitespace-only edits are not changes in the diff, so they must not be
  // changes here either.
  it("ignores a difference the diff normalises away", () => {
    const to = { ...from, instructions: "  old  " } as AgentContextSnapshot;
    expect(changedSnapshotKeys(from, to)).toEqual([]);
  });

  it("uses the resolver so a renamed skill counts as changed exactly once", () => {
    const to = { ...from, skill_ids: ["b"] } as AgentContextSnapshot;
    const resolveSkill = (id: string) => (id === "a" ? "triage" : "review");
    expect(changedSnapshotKeys(from, to, { resolveSkill })).toEqual([
      "skill_ids",
    ]);
  });
});
