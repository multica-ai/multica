// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createLandingDict } from "./dictionary";

describe("createLandingDict", () => {
  it.each([
    ["en", "/docs"],
    ["zh-Hans", "/docs/zh"],
    ["ko", "/docs/ko"],
    ["ja", "/docs/ja"],
    // French has no landing copy and reuses English, but its docs exist.
    ["fr", "/docs/fr"],
  ] as const)("links the %s footer to %s", (locale, docsHref) => {
    const links = createLandingDict(locale, true).footer.groups.resources.links;
    expect(links[0]?.href).toBe(docsHref);
  });
});
