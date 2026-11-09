// @vitest-environment node
import { describe, expect, it } from "vitest";
import { HERO_CARD_IDS } from "../site/demo/hero-demo";
import { createLandingDict } from "./dictionary";

describe("createLandingDict", () => {
  it("reuses the English copy for locales without their own dictionary", () => {
    expect(createLandingDict("fr")).toEqual(createLandingDict("en"));
  });

  it("serves translated copy for locales with a dictionary", () => {
    const en = createLandingDict("en");
    for (const locale of ["zh-Hans", "ja", "ko"] as const) {
      expect(createLandingDict(locale).faq.headline).not.toBe(en.faq.headline);
    }
  });

  it("translates the home page and site chrome instead of inheriting English", () => {
    const en = createLandingDict("en");
    for (const locale of ["zh-Hans", "ja", "ko"] as const) {
      const t = createLandingDict(locale);
      expect(t.home.hero.lead).not.toBe(en.home.hero.lead);
      expect(t.home.features.items.skills.body).not.toBe(en.home.features.items.skills.body);
      expect(t.home.logoWall.title).not.toBe(en.home.logoWall.title);
      expect(t.site.nav.resources).not.toBe(en.site.nav.resources);
      expect(t.site.cta.title).not.toBe(en.site.cta.title);
    }
  });

  it("gives every locale the same mockup copy as English, with a title for every hero card", () => {
    const en = createLandingDict("en");
    for (const locale of ["en", "zh-Hans", "ja", "ko"] as const) {
      const t = createLandingDict(locale);
      expect(shape(t.demo)).toEqual(shape(en.demo));
      for (const key of ["ship", "launch", "triage"] as const) {
        expect(Object.keys(t.demo.hero[key].cards).sort()).toEqual([...new Set(HERO_CARD_IDS[key])].sort());
      }
      if (locale !== "en") {
        expect(t.demo.hero.ship.focus.title).not.toBe(en.demo.hero.ship.focus.title);
        expect(t.demo.app.leaveComment).not.toBe(en.demo.app.leaveComment);
      }
    }
  });

  it("translates every page's title and share card", () => {
    const en = createLandingDict("en");
    for (const locale of ["zh-Hans", "ja", "ko"] as const) {
      const meta = createLandingDict(locale).meta;
      for (const [page, copy] of Object.entries(meta)) {
        const english = en.meta[page as keyof typeof meta];
        expect(copy.description).not.toBe(english.description);
        expect(copy.ogDescription).not.toBe(english.ogDescription);
      }
    }
  });
});

/** Every key path and array length, without the strings. */
function shape(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(shape);
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, shape(item)]));
  }
  return typeof value;
}
