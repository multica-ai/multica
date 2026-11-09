// @vitest-environment node
import { describe, expect, it } from "vitest";
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
});
