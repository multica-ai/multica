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
});
