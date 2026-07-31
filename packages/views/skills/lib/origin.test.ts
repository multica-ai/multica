import { describe, it, expect } from "vitest";
import type { SkillSummary } from "@multica/core/types";
import { canReimportSkill, isUpdatableOrigin } from "./origin";

function skill(config: Record<string, unknown>, createdBy: string | null): SkillSummary {
  return {
    id: "s1",
    workspace_id: "ws1",
    name: "n",
    description: "",
    config,
    created_by: createdBy,
    created_at: "",
    updated_at: "",
  } as SkillSummary;
}

describe("isUpdatableOrigin", () => {
  it("is true for URL sources with a source_url", () => {
    expect(isUpdatableOrigin({ type: "github", source_url: "https://github.com/a/b" })).toBe(true);
    expect(isUpdatableOrigin({ type: "clawhub", source_url: "https://clawhub.ai/a/b" })).toBe(true);
    expect(isUpdatableOrigin({ type: "skills_sh", source_url: "https://skills.sh/a" })).toBe(true);
  });
  it("is false without a source_url, for runtime_local, and for manual", () => {
    expect(isUpdatableOrigin({ type: "github" })).toBe(false);
    expect(isUpdatableOrigin({ type: "runtime_local", source_path: "/x" })).toBe(false);
    expect(isUpdatableOrigin({ type: "manual" })).toBe(false);
  });
});

describe("canReimportSkill", () => {
  const origin = { origin: { type: "github", source_url: "https://github.com/a/b" } };
  it("is true only for the creator of a URL-sourced skill", () => {
    expect(canReimportSkill(skill(origin, "u1"), "u1")).toBe(true);
  });
  it("is false for a non-creator (admins must edit in-app, not re-import)", () => {
    expect(canReimportSkill(skill(origin, "u1"), "u2")).toBe(false);
  });
  it("is false for a manual skill even for its creator", () => {
    expect(canReimportSkill(skill({}, "u1"), "u1")).toBe(false);
  });
  it("is false when there is no current user", () => {
    expect(canReimportSkill(skill(origin, "u1"), null)).toBe(false);
  });
});
