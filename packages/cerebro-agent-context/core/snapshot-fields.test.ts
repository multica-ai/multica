import { describe, it, expect } from "vitest";
import { snapshotToFields } from "./snapshot-fields";
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
