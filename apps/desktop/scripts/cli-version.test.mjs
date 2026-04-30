import { describe, expect, it } from "vitest";

import { normalizeCliVersion } from "./cli-version.mjs";

describe("normalizeCliVersion", () => {
  it("returns null for empty input", () => {
    expect(normalizeCliVersion("")).toBe(null);
    expect(normalizeCliVersion(null)).toBe(null);
    expect(normalizeCliVersion(undefined)).toBe(null);
  });

  it("accepts plain semver", () => {
    expect(normalizeCliVersion("0.2.11")).toBe("0.2.11");
  });

  it("strips a leading v", () => {
    expect(normalizeCliVersion("v0.2.11")).toBe("0.2.11");
  });

  it("preserves prerelease suffixes", () => {
    expect(normalizeCliVersion("v0.2.11-rc.1")).toBe("0.2.11-rc.1");
  });

  it("rejects invalid versions", () => {
    expect(normalizeCliVersion("main")).toBe(null);
    expect(normalizeCliVersion("0.2")).toBe(null);
    expect(normalizeCliVersion("version=0.2.11")).toBe(null);
  });
});
