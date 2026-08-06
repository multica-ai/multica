import { describe, it, expect } from "vitest";
import {
  snapshotToFields,
  changedSnapshotKeys,
  snapshotFieldsChanged,
} from "./snapshot-fields";
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

// FIR-3805 — an "always on" flip is a change of its own. The Skills tab keeps
// only the change requests and versions whose changed keys intersect
// ["skill_ids", "always_on_skill_ids"], so before this field existed a proposal
// that ONLY turned a skill always-on was stored server-side and then filtered
// out of the tab: nothing to review, nothing to approve, flag never applied.
describe("always-on skills as its own snapshot field", () => {
  const skill = "3833d007-b2b8-4160-a7cb-be642a97bef9";
  const off = {
    skill_ids: [skill],
  } as unknown as AgentContextSnapshot;
  const on = {
    skill_ids: [skill],
    always_on_skill_ids: [skill],
  } as unknown as AgentContextSnapshot;

  it("turning a bound skill always-on is a change the tab can see", () => {
    expect(snapshotFieldsChanged(off, on, ["always_on_skill_ids"])).toBe(true);
    expect(changedSnapshotKeys(off, on)).toEqual(["always_on_skill_ids"]);
  });

  // The server omits the key when the set is empty, so turning the flag OFF
  // reaches the frontend as an absent field — that must still read as a change,
  // or un-flagging stays invisible the same way flagging was.
  it("turning it off again is a change even though the server omits the key", () => {
    expect(snapshotFieldsChanged(on, off, ["always_on_skill_ids"])).toBe(true);
  });

  it("leaves the other fields alone when nothing else moved", () => {
    expect(changedSnapshotKeys(on, on)).toEqual([]);
    expect(snapshotFieldsChanged(off, on, ["skill_ids"])).toBe(false);
  });

  it("renders the skill name when a resolver is supplied", () => {
    const resolveSkill = (id: string) =>
      id === skill ? "issue-forstaaelse-rubrik" : id;
    const field = snapshotToFields(on, { resolveSkill }).find(
      (f) => f.key === "always_on_skill_ids",
    );
    expect(field?.label).toBe("Always-on skills");
    expect(field?.value).toBe("issue-forstaaelse-rubrik");
  });
});

describe("versioned runtime settings as labelled snapshot fields", () => {
  const from = {
    runtime_id: "runtime-1",
    runtime_config: {
      system_prompt_mode: "append",
      speed_mode: "fast",
      max_turns: 12,
      timeout_minutes: 30,
      openclaw_mode: "pro",
    },
    skill_ids: [],
  } as unknown as AgentContextSnapshot;

  it("renders governed controls separately and keeps only the rest in the raw field", () => {
    const fields = snapshotToFields(from);
    expect(fields.find((field) => field.key === "system_prompt_mode")?.value).toBe("append");
    expect(fields.find((field) => field.key === "runtime_id")?.value).toBe("runtime-1");
    expect(fields.find((field) => field.key === "speed_mode")?.value).toBe("fast");
    expect(fields.find((field) => field.key === "max_turns")?.value).toBe("12");
    expect(fields.find((field) => field.key === "timeout_minutes")?.value).toBe("30");
    expect(fields.find((field) => field.key === "runtime_config")?.value).toBe(
      JSON.stringify({ openclaw_mode: "pro" }, null, 2),
    );
  });

  it("reports a single labelled key instead of a generic runtime_config change", () => {
    const to = {
      ...from,
      runtime_config: {
        system_prompt_mode: "append",
        speed_mode: "fast",
        max_turns: 18,
        timeout_minutes: 30,
        openclaw_mode: "pro",
      },
    } as AgentContextSnapshot;
    expect(changedSnapshotKeys(from, to)).toEqual(["max_turns"]);
  });

  it("reports Engine as a governed snapshot change", () => {
    expect(changedSnapshotKeys(from, { ...from, runtime_id: "runtime-2" })).toEqual([
      "runtime_id",
    ]);
  });
});
