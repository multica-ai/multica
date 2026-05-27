import { describe, expect, it } from "vitest";
import { resolveInitialMobileLocale } from "./mobile-locale-utils";

describe("mobile locale", () => {
  it("defaults to zh-Hans when no user choice exists", () => {
    expect(resolveInitialMobileLocale(null)).toBe("zh-Hans");
    expect(resolveInitialMobileLocale("")).toBe("zh-Hans");
  });

  it("keeps an explicit user choice", () => {
    expect(resolveInitialMobileLocale("en")).toBe("en");
    expect(resolveInitialMobileLocale("zh-Hans")).toBe("zh-Hans");
  });
});
