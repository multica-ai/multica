/**
 * Generated avatar derivation (MAKE-291) — web canonical suite.
 *
 * Tests the shared utility in packages/ui/lib/avatar-seed.ts (ui has no test
 * runner of its own; views is where ui-consuming logic is exercised, matching
 * avatar-upload-control.test.tsx). The fixture vectors below are duplicated
 * verbatim in apps/mobile/lib/avatar-seed.test.ts — the mobile copy of this
 * algorithm may only diverge if BOTH suites are updated, which is exactly the
 * coupling the fixture lock is meant to catch. The scene fingerprints pin the
 * rendered geometry AND gradient defs, not just the trait table: a scene
 * change must fail both suites too.
 */
import { describe, expect, it } from "vitest";
import {
  ROLE_ACCESSORIES,
  buildAvatarScene,
  deriveAvatarDesign,
  deriveBearVariant,
  deriveAndroidVariant,
  deriveDragonVariant,
  deriveIllustratedVariant,
  deriveFoxVariant,
  deriveOwlVariant,
  parseGeneratedSeed,
  suggestRoleAccessory,
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

describe("deriveFoxVariant", () => {
  it("maps persisted seeds to the three approved Fox variants", () => {
    expect(deriveAvatarDesign("fox-26").archetype).toBe("fox");
    expect(deriveAvatarDesign("fox-1").archetype).toBe("fox");
    expect(deriveAvatarDesign("fox-40").archetype).toBe("fox");

    expect(deriveFoxVariant("fox-26")).toBe("general");
    expect(deriveFoxVariant("fox-1")).toBe("developer");
    expect(deriveFoxVariant("fox-40")).toBe("auditor");
  });

  it("is stable and depends only on avatar_seed", () => {
    const seed = "abcdef01-2345-6789-abcd-ef0123456789";
    const first = deriveFoxVariant(seed);
    for (let i = 0; i < 20; i++) {
      expect(deriveFoxVariant(seed)).toBe(first);
    }
  });
});

describe("deriveBearVariant", () => {
  it("maps persisted Bear seeds to the three approved variants", () => {
    expect(deriveAvatarDesign("bear-2").archetype).toBe("bear");
    expect(deriveAvatarDesign("bear-10").archetype).toBe("bear");
    expect(deriveAvatarDesign("bear-9").archetype).toBe("bear");

    expect(deriveBearVariant("bear-2")).toBe("general");
    expect(deriveBearVariant("bear-10")).toBe("developer");
    expect(deriveBearVariant("bear-9")).toBe("auditor");
  });

  it("is stable and depends only on avatar_seed", () => {
    const seed = "22222222-2222-2222-2222-222222222222";
    const first = deriveBearVariant(seed);
    for (let i = 0; i < 20; i++) {
      expect(deriveBearVariant(seed)).toBe(first);
    }
  });
});

describe("deriveOwlVariant", () => {
  it("maps persisted Owl seeds to the three approved variants", () => {
    expect(deriveAvatarDesign("owl-30").archetype).toBe("owl");
    expect(deriveAvatarDesign("owl-1").archetype).toBe("owl");
    expect(deriveAvatarDesign("owl-10").archetype).toBe("owl");

    expect(deriveOwlVariant("owl-30")).toBe("general");
    expect(deriveOwlVariant("owl-1")).toBe("developer");
    expect(deriveOwlVariant("owl-10")).toBe("auditor");
  });

  it("is stable and depends only on avatar_seed", () => {
    const seed = "00000000-0000-0000-0000-000000000000";
    const first = deriveOwlVariant(seed);
    for (let i = 0; i < 20; i++) {
      expect(deriveOwlVariant(seed)).toBe(first);
    }
  });
});

describe("deriveDragonVariant", () => {
  it("maps persisted Dragon seeds to the three approved variants", () => {
    expect(deriveAvatarDesign("dragon-8").archetype).toBe("dragon");
    expect(deriveAvatarDesign("dragon-2").archetype).toBe("dragon");
    expect(deriveAvatarDesign("dragon-18").archetype).toBe("dragon");

    expect(deriveDragonVariant("dragon-8")).toBe("general");
    expect(deriveDragonVariant("dragon-2")).toBe("developer");
    expect(deriveDragonVariant("dragon-18")).toBe("auditor");
  });

  it("is stable and depends only on avatar_seed", () => {
    const seed = "00000000-0000-0000-0000-000000000000";
    const first = deriveDragonVariant(seed);
    for (let i = 0; i < 20; i++) {
      expect(deriveDragonVariant(seed)).toBe(first);
    }
  });
});

describe("deriveAndroidVariant", () => {
  it("maps persisted Android seeds to the three approved variants", () => {
    expect(deriveAvatarDesign("android-0").archetype).toBe("android");
    expect(deriveAvatarDesign("android-27").archetype).toBe("android");
    expect(deriveAvatarDesign("android-15").archetype).toBe("android");

    expect(deriveAndroidVariant("android-0")).toBe("general");
    expect(deriveAndroidVariant("android-27")).toBe("developer");
    expect(deriveAndroidVariant("android-15")).toBe("auditor");
  });

  it("is stable and depends only on avatar_seed", () => {
    const seed = "00000000-0000-0000-0000-000000000000";
    const first = deriveAndroidVariant(seed);
    for (let i = 0; i < 20; i++) {
      expect(deriveAndroidVariant(seed)).toBe(first);
    }
  });

  it("keeps existing archetype assignments unchanged by the Android family", () => {
    // Snapshot captured before Android integration; archetype hashing must not drift.
    const expected: Record<string, string> = {
      "fox-0": "android", "fox-1": "fox", "fox-2": "owl", "fox-3": "dragon",
      "fox-26": "fox", "bear-2": "bear", "owl-30": "owl", "dragon-8": "dragon",
      "android-0": "android", "android-1": "fox",
      "11111111-1111-1111-1111-111111111111": "dragon",
      "22222222-2222-2222-2222-222222222222": "bear",
    };
    for (const [seed, archetype] of Object.entries(expected)) {
      expect(deriveAvatarDesign(seed).archetype, seed).toBe(archetype);
    }
    expect(deriveFoxVariant("fox-26")).toBe("general");
    expect(deriveBearVariant("bear-2")).toBe("general");
    expect(deriveOwlVariant("owl-30")).toBe("general");
    expect(deriveDragonVariant("dragon-8")).toBe("general");
  });

  it("derives a variant only for illustrated archetypes", () => {
    expect(deriveIllustratedVariant("android", "android-0")).toBe("general");
    expect(deriveIllustratedVariant("kraken", "x")).toBeNull();
  });
});

describe("deriveAvatarDesign", () => {
  // Fixture vectors shared with apps/mobile/lib/avatar-seed.test.ts. They pin
  // the algorithm: if either copy drifts, one suite fails. Four seeds, four
  // different archetypes — identity must not be color-only.
  const fixtures: Array<[string, ReturnType<typeof deriveAvatarDesign>]> = [
    [
      "11111111-1111-1111-1111-111111111111",
      {
        archetype: "dragon",
        palette: 2,
        hue: 150,
        hue2: 180,
        accentHue: 345,
        eyes: 0,
        expression: 0,
        accessory: 5,
        pattern: 3,
        detail: 1,
      },
    ],
    [
      "22222222-2222-2222-2222-222222222222",
      {
        archetype: "bear",
        palette: 2,
        hue: 143,
        hue2: 173,
        accentHue: 338,
        eyes: 1,
        expression: 2,
        accessory: 1,
        pattern: 0,
        detail: 1,
      },
    ],
    [
      "00000000-0000-0000-0000-000000000000",
      {
        archetype: "owl",
        palette: 4,
        hue: 253,
        hue2: 283,
        accentHue: 88,
        eyes: 0,
        expression: 1,
        accessory: 1,
        pattern: 2,
        detail: 1,
      },
    ],
    [
      "abcdef01-2345-6789-abcd-ef0123456789",
      {
        archetype: "fox",
        palette: 5,
        hue: 33,
        hue2: 63,
        accentHue: 228,
        eyes: 0,
        expression: 0,
        accessory: 5,
        pattern: 2,
        detail: 2,
      },
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
    // and the pinned fixtures land on four DIFFERENT archetypes — identity
    // must not rest on color alone (silhouette differs too).
    const designs = fixtures.map(([seed]) => deriveAvatarDesign(seed));
    for (let i = 0; i < designs.length; i++) {
      for (let j = i + 1; j < designs.length; j++) {
        expect(designs[i]).not.toEqual(designs[j]);
      }
    }
    const archetypes = new Set(designs.map((d) => d.archetype));
    expect(archetypes.size).toBe(designs.length);
  });

  it("spreads all five archetypes across a seed sample", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 500; i++) {
      seen.add(deriveAvatarDesign(`sample-${i}`).archetype);
    }
    expect([...seen].sort()).toEqual([
      "android",
      "bear",
      "dragon",
      "fox",
      "owl",
    ]);
  });

  it("emits only valid design fields", () => {
    for (const [seed] of fixtures) {
      const d = deriveAvatarDesign(seed);
      expect(["fox", "bear", "owl", "dragon", "android"]).toContain(d.archetype);
      expect(d.palette).toBeGreaterThanOrEqual(0);
      expect(d.palette).toBeLessThan(6);
      expect(d.hue).toBeGreaterThanOrEqual(0);
      expect(d.hue).toBeLessThan(360);
      expect(d.hue2).toBeGreaterThanOrEqual(0);
      expect(d.hue2).toBeLessThan(360);
      expect(d.accentHue).toBeGreaterThanOrEqual(0);
      expect(d.accentHue).toBeLessThan(360);
      expect([0, 1, 2, 3]).toContain(d.eyes);
      expect([0, 1, 2]).toContain(d.expression);
      expect(d.accessory).toBeGreaterThanOrEqual(0);
      expect(d.accessory).toBeLessThan(ROLE_ACCESSORIES.length);
      expect([0, 1, 2, 3]).toContain(d.pattern);
      expect([0, 1, 2]).toContain(d.detail);
      // The two hues must actually differ, or every avatar in that band
      // collapses to a flat single-hue field.
      expect(d.hue).not.toBe(d.hue2);
      // Palette hues stay coordinated: hue2 rides ~30° above hue.
      expect(d.hue2 - d.hue).toBeGreaterThanOrEqual(20);
      expect(d.hue2 - d.hue).toBeLessThanOrEqual(40);
      // Accent stays complementary so gadgets pop on the backdrop.
      const accDistance = Math.abs(d.accentHue - d.hue2);
      expect(Math.min(accDistance, 360 - accDistance)).toBeGreaterThanOrEqual(150);
    }
  });

  it("handles arbitrary non-UUID seeds without throwing", () => {
    expect(() => deriveAvatarDesign("")).not.toThrow();
    expect(() => deriveAvatarDesign("not-a-uuid")).not.toThrow();
    expect(() => deriveAvatarDesign("🔥🧠")).not.toThrow();
  });
});

describe("buildAvatarScene", () => {
  // Scene fingerprints (defs + shapes + FNV of the stable scene JSON) shared
  // with apps/mobile/lib/avatar-seed.test.ts — pin the rendered geometry and
  // gradients themselves.
  const sceneFixtures: Array<[string, { defs: number; shapes: number; fnv: string }]> = [
    ["11111111-1111-1111-1111-111111111111", { defs: 5, shapes: 63, fnv: "3d058e8c" }],
    ["22222222-2222-2222-2222-222222222222", { defs: 5, shapes: 44, fnv: "e4960072" }],
    ["00000000-0000-0000-0000-000000000000", { defs: 5, shapes: 63, fnv: "f6bb277a" }],
    ["abcdef01-2345-6789-abcd-ef0123456789", { defs: 5, shapes: 66, fnv: "066c3c5e" }],
  ];

  function sceneFingerprint(seed: string): { defs: number; shapes: number; fnv: string } {
    const scene = buildAvatarScene(deriveAvatarDesign(seed));
    const json = JSON.stringify(scene);
    let h = 0x811c9dc5;
    for (let i = 0; i < json.length; i++) {
      h ^= json.charCodeAt(i);
      h = Math.imul(h, 0x01000193);
    }
    return {
      defs: scene.defs.length,
      shapes: scene.shapes.length,
      fnv: (h >>> 0).toString(16).padStart(8, "0"),
    };
  }

  it.each(sceneFixtures)("pins the scene geometry for %s", (seed, want) => {
    expect(sceneFingerprint(seed)).toEqual(want);
  });

  it("is deterministic — same seed, same scene, byte for byte", () => {
    const seed = "55555555-5555-5555-5555-555555555555";
    const a = JSON.stringify(buildAvatarScene(deriveAvatarDesign(seed)));
    for (let i = 0; i < 5; i++) {
      expect(JSON.stringify(buildAvatarScene(deriveAvatarDesign(seed)))).toBe(a);
    }
  });

  it("emits a background, renderable primitives, and resolvable gradients", () => {
    const seeds = [
      "11111111-1111-1111-1111-111111111111",
      "22222222-2222-2222-2222-222222222222",
      "66666666-6666-6666-6666-666666666666",
    ];
    for (const seed of seeds) {
      const scene = buildAvatarScene(deriveAvatarDesign(seed));
      expect(scene.defs.length).toBeGreaterThan(0);
      expect(scene.shapes.length).toBeGreaterThan(20);
      const first = scene.shapes[0];
      expect(first?.type).toBe("rect");
      expect(first?.fill).toBeDefined();

      const defIds = new Set(scene.defs.map((g) => g.id));
      // Gradient ids are unique within the scene.
      expect(defIds.size).toBe(scene.defs.length);
      // Every gradient has ordered paint stops.
      for (const g of scene.defs) {
        expect(g.stops.length).toBeGreaterThanOrEqual(2);
        for (const s of g.stops) {
          expect(s.offset).toBeGreaterThanOrEqual(0);
          expect(s.offset).toBeLessThanOrEqual(1);
          expect(s.color.length).toBeGreaterThan(0);
        }
      }
      for (const s of scene.shapes) {
        expect(["rect", "circle", "ellipse", "path", "line"]).toContain(s.type);
        // Every shape must carry paint: a fill, or a stroke (the renderer
        // defaults stroke-only shapes to fill:none so nothing paints black).
        expect(s.fill !== undefined || s.stroke !== undefined).toBe(true);
        // Every url(#…) reference must resolve inside this scene's own defs.
        for (const v of [s.fill, s.stroke]) {
          if (v && v.startsWith("url(#")) {
            expect(defIds.has(v.slice(5, -1))).toBe(true);
          }
        }
      }
    }
  });

  it("builds scenes for weird seeds without throwing", () => {
    for (const seed of ["", "not-a-uuid", "🔥🧠"]) {
      expect(() => buildAvatarScene(deriveAvatarDesign(seed))).not.toThrow();
    }
  });

  it("accessoryOverride changes the scene (sheet/testing affordance only)", () => {
    const design = deriveAvatarDesign("77777777-7777-7777-7777-777777777777");
    const base = JSON.stringify(buildAvatarScene(design));
    const overridden = JSON.stringify(
      buildAvatarScene(design, { accessoryOverride: design.accessory + 1 }),
    );
    expect(overridden).not.toBe(base);
    // Without the option the seed accessory applies (renderers never pass it).
    expect(JSON.stringify(buildAvatarScene(design))).toBe(base);
  });
});

describe("suggestRoleAccessory", () => {
  it("returns the neutral default when metadata is empty", () => {
    expect(suggestRoleAccessory({})).toBe("headset");
    expect(suggestRoleAccessory({ description: "", instructions: null })).toBe("headset");
  });

  it("maps durable role metadata into the accessory set", () => {
    // [metadata, allowed picks] — the helper reads description/instructions
    // only (the type has no name field: name-only inference is disallowed).
    const cases: Array<[{ description: string }, readonly string[]]> = [
      [{ description: "Writes TypeScript services" }, ["headphones", "goggles"]],
      [{ description: "Operates cloud kubernetes clusters and infrastructure" }, ["visor", "headset"]],
      [{ description: "Compliance oversight for financial reports" }, ["glasses", "notebook"]],
      [{ description: "Studies literature and discovers experiments" }, ["book", "glasses"]],
      [{ description: "Coordinates project plans for the team" }, ["headset", "notebook"]],
      [{ description: "Automation workflows with cron triggers" }, ["antenna", "visor"]],
    ];
    for (const [meta, allowed] of cases) {
      const got = suggestRoleAccessory(meta);
      expect(ROLE_ACCESSORIES as readonly string[]).toContain(got);
      expect(allowed).toContain(got);
    }
  });

  it("is deterministic for the same metadata", () => {
    const meta = { description: "cloud kubernetes operations", instructions: "deploy runtimes" };
    const first = suggestRoleAccessory(meta);
    for (let i = 0; i < 5; i++) {
      expect(suggestRoleAccessory(meta)).toBe(first);
    }
  });
});
