// @vitest-environment node
import { describe, expect, it } from "vitest";
import { docsLocalePrefix } from "./docs-locale";

describe("docsLocalePrefix", () => {
  it.each([
    ["en", ""],
    [undefined, ""],
    ["zh-Hans", "/zh"],
    ["zh-Hant", "/zh-Hant"],
    ["ja", "/ja"],
    ["ko", "/ko"],
    ["fr", "/fr"],
  ])("maps %s to %j", (language, expected) => {
    expect(docsLocalePrefix(language)).toBe(expected);
  });
});
