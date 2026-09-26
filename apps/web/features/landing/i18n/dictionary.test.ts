// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createLandingDict } from "./dictionary";

describe("createLandingDict", () => {
  it.each([
    ["en", "/docs"],
    ["zh-Hans", "/docs/zh"],
    ["ko", "/docs/ko"],
    ["ja", "/docs/ja"],
    ["fr", "/docs/fr"],
  ] as const)("links the %s footer to %s", (locale, docsHref) => {
    const links = createLandingDict(locale, true).footer.groups.resources.links;
    expect(links[0]?.href).toBe(docsHref);
  });

  it("serves French its own copy rather than the English fallback", () => {
    const en = createLandingDict("en", true);
    const fr = createLandingDict("fr", true);
    expect(fr.footer.groups.resources.label).not.toBe(
      en.footer.groups.resources.label,
    );
    expect(fr.changelog.entries.map((e) => e.version)).toEqual(
      en.changelog.entries.map((e) => e.version),
    );
  });
});
