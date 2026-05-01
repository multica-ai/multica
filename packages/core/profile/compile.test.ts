import { describe, expect, it } from "vitest";
import { compileProfile } from "./compile";
import { PERSONAS, type Profile, validateProfile } from "./schema";
import { buildDefaultProfile } from "./presets";

describe("compileProfile", () => {
  it("is deterministic — same input produces identical output", () => {
    const profile = buildDefaultProfile("utalmodig");
    const a = compileProfile(profile);
    const b = compileProfile(profile);
    expect(a).toBe(b);
  });

  it("does not mutate input", () => {
    const profile = buildDefaultProfile("ekspert");
    const before = JSON.stringify(profile);
    compileProfile(profile, { displayName: "Jens" });
    expect(JSON.stringify(profile)).toBe(before);
  });

  for (const persona of PERSONAS) {
    it(`snapshot — persona "${persona}" (default profile)`, () => {
      const profile = buildDefaultProfile(persona);
      const compiled = compileProfile(profile, { displayName: "Jens" });
      expect(compiled).toMatchSnapshot();
    });
  }

  it("snapshot — utalmodig in english renders english labels", () => {
    const profile: Profile = { ...buildDefaultProfile("utalmodig"), language: "en" };
    expect(compileProfile(profile, { displayName: "Jens" })).toMatchSnapshot();
  });

  it("omits AVOID section when there are no anti-patterns", () => {
    const profile: Profile = { ...buildDefaultProfile("ekspert"), antiPatterns: [] };
    const compiled = compileProfile(profile);
    expect(compiled).not.toContain("AVOID");
  });

  it("renders header without display name when none provided", () => {
    const profile = buildDefaultProfile("grundig");
    const compiled = compileProfile(profile);
    expect(compiled.startsWith("USER: Den grundige")).toBe(true);
  });

  it("low-end sliders produce terse style + high-autonomy line set", () => {
    const profile: Profile = {
      ...buildDefaultProfile("utalmodig"),
      lengthPref: 0,
      autonomyPref: 100,
      techPref: 0,
    };
    const compiled = compileProfile(profile);
    expect(compiled).toContain("1–3 sætninger");
    expect(compiled).toContain("Reversibelt: bare kør");
    expect(compiled).toContain("arch:surface");
  });

  it("max sliders produce verbose style + cautious autonomy + deep tech", () => {
    const profile: Profile = {
      ...buildDefaultProfile("grundig"),
      lengthPref: 100,
      autonomyPref: 0,
      techPref: 100,
    };
    const compiled = compileProfile(profile);
    expect(compiled).toContain("Forklar ræsonnement");
    expect(compiled).toContain("Bekræft plan");
    expect(compiled).toContain("arch:deep");
  });

  it("snapshot — empty anti-patterns + mid-range sliders", () => {
    const profile: Profile = {
      persona: "ekspert",
      language: "da",
      lengthPref: 50,
      autonomyPref: 50,
      techPref: 50,
      antiPatterns: [],
    };
    expect(compileProfile(profile, { displayName: "Jens" })).toMatchSnapshot();
  });

  it("snapshot — max anti-patterns at boundary", () => {
    const profile: Profile = {
      ...buildDefaultProfile("utalmodig"),
      antiPatterns: Array.from({ length: 20 }, (_, i) => `anti-pattern-${i + 1}`),
    };
    expect(validateProfile(profile)).toEqual([]);
    expect(compileProfile(profile, { displayName: "Jens" })).toMatchSnapshot();
  });
});
