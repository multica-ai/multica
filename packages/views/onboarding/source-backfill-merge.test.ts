import { describe, expect, it } from "vitest";
import { mergedQuestionnairePatch } from "./source-backfill-merge";

describe("mergedQuestionnairePatch", () => {
  it("preserves role / use_case when overlaying a fresh source", () => {
    const stored: Record<string, unknown> = {
      role: "engineer",
      role_other: null,
      role_skipped: false,
      use_case: ["ship_code", "plan_research"],
      use_case_other: null,
      use_case_skipped: false,
      version: 2,
    };
    const out = mergedQuestionnairePatch(stored, {
      source: ["search"],
      source_other: null,
      source_skipped: false,
    });
    expect(out.source).toEqual(["search"]);
    expect(out.role).toBe("engineer");
    expect(out.use_case).toEqual(["ship_code", "plan_research"]);
    expect(out.version).toBe(2);
  });

  it("preserves an existing source_skipped → false transition while keeping other slots", () => {
    const stored: Record<string, unknown> = {
      source_skipped: true,
      role: "founder",
      use_case: ["manage_team"],
    };
    const out = mergedQuestionnairePatch(stored, {
      source: ["friends_colleagues"],
      source_other: null,
      source_skipped: false,
    });
    expect(out.source_skipped).toBe(false);
    expect(out.role).toBe("founder");
    expect(out.use_case).toEqual(["manage_team"]);
  });

  it("explicit skip preserves role / use_case but writes source_skipped=true", () => {
    const stored: Record<string, unknown> = {
      role: "designer",
      use_case: ["plan_research"],
    };
    const out = mergedQuestionnairePatch(stored, {
      source: [],
      source_other: null,
      source_skipped: true,
    });
    expect(out.source).toEqual([]);
    expect(out.source_skipped).toBe(true);
    expect(out.role).toBe("designer");
    expect(out.use_case).toEqual(["plan_research"]);
  });

  it("coerces legacy single-string use_case into a one-element array", () => {
    const stored: Record<string, unknown> = {
      role: "engineer",
      use_case: "ship_code",
    };
    const out = mergedQuestionnairePatch(stored, {
      source: ["search"],
      source_other: null,
      source_skipped: false,
    });
    expect(out.use_case).toEqual(["ship_code"]);
  });

  it("defaults missing role / use_case to null / [] without throwing", () => {
    const out = mergedQuestionnairePatch(null, {
      source: ["search"],
      source_other: null,
      source_skipped: false,
    });
    expect(out.role).toBeNull();
    expect(out.role_other).toBeNull();
    expect(out.role_skipped).toBe(false);
    expect(out.use_case).toEqual([]);
    expect(out.version).toBe(2);
  });

  it("treats undefined / non-string fields as their type-appropriate default", () => {
    const stored: Record<string, unknown> = {
      role: 123,
      role_other: 456,
      role_skipped: "yes",
      use_case: null,
      use_case_other: false,
      use_case_skipped: 1,
    };
    const out = mergedQuestionnairePatch(stored, {
      source: [],
      source_other: null,
      source_skipped: false,
    });
    expect(out.role).toBeNull();
    expect(out.role_other).toBeNull();
    expect(out.role_skipped).toBe(false);
    expect(out.use_case).toEqual([]);
    expect(out.use_case_other).toBeNull();
    expect(out.use_case_skipped).toBe(false);
  });

  it("forces version to 2 regardless of stored value", () => {
    const stored: Record<string, unknown> = { version: 1 };
    const out = mergedQuestionnairePatch(stored, {
      source: ["search"],
      source_other: null,
      source_skipped: false,
    });
    expect(out.version).toBe(2);
  });
});
