import { describe, expect, it } from "vitest";
import { RTL_LOCALES, getDirection } from "./direction";

describe("getDirection", () => {
  it("returns ltr for en", () => {
    expect(getDirection("en")).toBe("ltr");
  });

  it("returns ltr for zh-Hans", () => {
    expect(getDirection("zh-Hans")).toBe("ltr");
  });

  it("returns rtl for he", () => {
    expect(getDirection("he")).toBe("rtl");
  });
});

describe("RTL_LOCALES", () => {
  it("contains he", () => {
    expect(RTL_LOCALES.has("he")).toBe(true);
  });

  it("does not contain ltr locales", () => {
    expect(RTL_LOCALES.has("en")).toBe(false);
    expect(RTL_LOCALES.has("zh-Hans")).toBe(false);
  });
});
