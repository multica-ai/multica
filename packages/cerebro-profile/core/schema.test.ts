import { describe, expect, it } from "vitest";
import {
  ANTI_PATTERN_MAX_LENGTH,
  ANTI_PATTERNS_MAX_COUNT,
  profileFromRow,
  type Profile,
  validateProfile,
} from "./schema";
import { buildDefaultProfile } from "./presets";

describe("validateProfile", () => {
  it("returns no errors for a default profile", () => {
    expect(validateProfile(buildDefaultProfile("utalmodig"))).toEqual([]);
  });

  it("flags invalid persona", () => {
    const profile = { ...buildDefaultProfile("utalmodig"), persona: "bogus" } as unknown as Profile;
    expect(validateProfile(profile)).toContainEqual({ kind: "invalid_persona", got: "bogus" });
  });

  it("flags invalid language", () => {
    const profile = { ...buildDefaultProfile("utalmodig"), language: "de" } as unknown as Profile;
    expect(validateProfile(profile)).toContainEqual({ kind: "invalid_language", got: "de" });
  });

  it("flags slider values outside 0..100", () => {
    const profile: Profile = { ...buildDefaultProfile("utalmodig"), lengthPref: -1, techPref: 101 };
    const errors = validateProfile(profile);
    expect(errors).toContainEqual({ kind: "slider_out_of_range", field: "lengthPref", got: -1 });
    expect(errors).toContainEqual({ kind: "slider_out_of_range", field: "techPref", got: 101 });
  });

  it("flags non-integer slider values", () => {
    const profile: Profile = { ...buildDefaultProfile("utalmodig"), autonomyPref: 50.5 };
    expect(validateProfile(profile)).toContainEqual({
      kind: "slider_out_of_range",
      field: "autonomyPref",
      got: 50.5,
    });
  });

  it("flags too many anti-patterns", () => {
    const profile: Profile = {
      ...buildDefaultProfile("utalmodig"),
      antiPatterns: Array.from({ length: ANTI_PATTERNS_MAX_COUNT + 1 }, (_, i) => `p${i}`),
    };
    expect(validateProfile(profile)).toContainEqual({
      kind: "too_many_anti_patterns",
      count: ANTI_PATTERNS_MAX_COUNT + 1,
      max: ANTI_PATTERNS_MAX_COUNT,
    });
  });

  it("flags anti-patterns over max length", () => {
    const tooLong = "x".repeat(ANTI_PATTERN_MAX_LENGTH + 1);
    const profile: Profile = { ...buildDefaultProfile("utalmodig"), antiPatterns: [tooLong] };
    expect(validateProfile(profile)).toContainEqual({
      kind: "anti_pattern_too_long",
      index: 0,
      length: ANTI_PATTERN_MAX_LENGTH + 1,
      max: ANTI_PATTERN_MAX_LENGTH,
    });
  });
});

describe("profileFromRow", () => {
  it("maps DB snake_case row to camelCase Profile", () => {
    const row = {
      user_id: "u1",
      persona: "grundig" as const,
      language: "da" as const,
      length_pref: 75,
      autonomy_pref: 30,
      tech_pref: 80,
      anti_patterns: ["foo", "bar"],
      updated_at: "2026-05-01T11:00:00Z",
    };
    expect(profileFromRow(row)).toEqual({
      persona: "grundig",
      language: "da",
      lengthPref: 75,
      autonomyPref: 30,
      techPref: 80,
      antiPatterns: ["foo", "bar"],
    });
  });
});
