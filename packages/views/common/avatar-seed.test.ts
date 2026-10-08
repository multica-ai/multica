/**
 * Generated avatar derivation (MAKE-291) — web canonical suite.
 *
 * Tests the shared utility in packages/ui/lib/avatar-seed.ts (ui has no test
 * runner of its own; views is where ui-consuming logic is exercised, matching
 * avatar-upload-control.test.tsx). The fixture vectors below are duplicated
 * verbatim in apps/mobile/lib/avatar-seed.test.ts — the mobile copy of this
 * algorithm may only diverge if BOTH suites are updated, which is exactly the
 * coupling the fixture lock is meant to catch.
 */
import { describe, expect, it } from "vitest";
import {
  deriveAvatarDesign,
  parseGeneratedSeed,
} from "@multica/ui/lib/avatar-seed";

describe("parseGeneratedSeed", () => {
  it("extracts the seed from a gen: marker", () => {
    expect(parseGeneratedSeed("gen:11111111-1111-1111-1111-111111111111")).toBe(
      "11111111-1111-1111-1111-111111111111",
    );
  });

  it("trims surrounding whitespace inside the marker", () => {
    expect(parseGeneratedSeed("gen:  abc  ")).toBe("abc");
  });

  it("returns null for every non-marker value", () => {
    expect(parseGeneratedSeed("emoji:🐙")).toBeNull();
    expect(parseGeneratedSeed("https://cdn.example.com/a.png")).toBeNull();
    expect(parseGeneratedSeed("data:image/svg+xml,%3Csvg%3E")).toBeNull();
    expect(parseGeneratedSeed("gen:")).toBeNull();
    expect(parseGeneratedSeed("gen:   ")).toBeNull();
    expect(parseGeneratedSeed("")).toBeNull();
    expect(parseGeneratedSeed(null)).toBeNull();
    expect(parseGeneratedSeed(undefined)).toBeNull();
  });
});

describe("deriveAvatarDesign", () => {
  // Fixture vectors shared with apps/mobile/lib/avatar-seed.test.ts. They pin
  // the algorithm: if either copy drifts, one suite fails.
  const fixtures: Array<[string, ReturnType<typeof deriveAvatarDesign>]> = [
    [
      "11111111-1111-1111-1111-111111111111",
      { hue: 113, hue2: 176, pattern: 0, face: 2, antenna: false },
    ],
    [
      "22222222-2222-2222-2222-222222222222",
      { hue: 41, hue2: 139, pattern: 2, face: 0, antenna: false },
    ],
    [
      "00000000-0000-0000-0000-000000000000",
      { hue: 177, hue2: 240, pattern: 2, face: 2, antenna: true },
    ],
    [
      "abcdef01-2345-6789-abcd-ef0123456789",
      { hue: 185, hue2: 229, pattern: 1, face: 1, antenna: false },
    ],
  ];

  it.each(fixtures)("derives the pinned design for %s", (seed, want) => {
    expect(deriveAvatarDesign(seed)).toEqual(want);
  });

  it("is deterministic — the same seed always yields the same design", () => {
    const seed = "33333333-3333-3333-3333-333333333333";
    const first = deriveAvatarDesign(seed);
    for (let i = 0; i < 20; i++) {
      expect(deriveAvatarDesign(seed)).toEqual(first);
    }
  });

  it("depends only on the seed — a rename (name never passed) cannot affect it", () => {
    // The function signature takes ONLY the seed; there is no name/provider
    // parameter to thread identity through. Two calls, no other input.
    const seed = "44444444-4444-4444-4444-444444444444";
    expect(deriveAvatarDesign(seed)).toEqual(deriveAvatarDesign(seed));
  });

  it("produces distinguishable designs for different seeds", () => {
    // Pairwise: the four fixture seeds must not collide on the whole design,
    // and in particular must differ in hue (the dominant visual signal).
    const designs = fixtures.map(([seed]) => deriveAvatarDesign(seed));
    for (let i = 0; i < designs.length; i++) {
      for (let j = i + 1; j < designs.length; j++) {
        expect(designs[i]).not.toEqual(designs[j]);
      }
    }
    const hues = new Set(designs.map((d) => d.hue));
    expect(hues.size).toBe(designs.length);
  });

  it("emits only valid design fields", () => {
    for (const [seed] of fixtures) {
      const d = deriveAvatarDesign(seed);
      expect(d.hue).toBeGreaterThanOrEqual(0);
      expect(d.hue).toBeLessThan(360);
      expect(d.hue2).toBeGreaterThanOrEqual(0);
      expect(d.hue2).toBeLessThan(360);
      expect([0, 1, 2, 3]).toContain(d.pattern);
      expect([0, 1, 2]).toContain(d.face);
      expect(typeof d.antenna).toBe("boolean");
      // The two hues must actually differ, or every avatar in that band
      // collapses to a flat single-hue field.
      expect(d.hue).not.toBe(d.hue2);
    }
  });

  it("handles arbitrary non-UUID seeds without throwing", () => {
    expect(() => deriveAvatarDesign("")).not.toThrow();
    expect(() => deriveAvatarDesign("not-a-uuid")).not.toThrow();
    expect(() => deriveAvatarDesign("🔥🧠")).not.toThrow();
  });
});
