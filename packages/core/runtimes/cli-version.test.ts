import { describe, it, expect } from "vitest";
import { checkQuickCreateCliVersion, isCliVersionNewer } from "./cli-version";

describe("checkQuickCreateCliVersion", () => {
  it("returns ok for a tagged release at or above the minimum", () => {
    expect(checkQuickCreateCliVersion("v0.2.21").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.3.1").state).toBe("ok");
  });

  it("returns too_old for a tagged release below the minimum", () => {
    expect(checkQuickCreateCliVersion("v0.2.20").state).toBe("too_old");
    expect(checkQuickCreateCliVersion("v0.2.15").state).toBe("too_old");
  });

  it("returns missing for empty or unparsable input", () => {
    expect(checkQuickCreateCliVersion("").state).toBe("missing");
    expect(checkQuickCreateCliVersion(undefined).state).toBe("missing");
    expect(checkQuickCreateCliVersion("not-a-version").state).toBe("missing");
  });

  it("treats git-describe dev builds as ok regardless of base tag", () => {
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935").state).toBe("ok");
    expect(checkQuickCreateCliVersion("v0.2.15-235-gdaf0e935-dirty").state).toBe("ok");
    expect(checkQuickCreateCliVersion("0.1.0-1-gabc1234").state).toBe("ok");
  });
});

describe("isCliVersionNewer", () => {
  it("compares major, minor, and patch before commit counts", () => {
    expect(isCliVersionNewer("v0.2.12-1-abcd", "v0.2.11-999-ffff")).toBe(true);
    expect(isCliVersionNewer("v0.3.0-1-abcd", "v0.2.99-999-ffff")).toBe(true);
    expect(isCliVersionNewer("v0.2.10-999-ffff", "v0.2.11-1-abcd")).toBe(false);
  });

  it("uses commit count when the first three version parts are equal", () => {
    expect(isCliVersionNewer("v0.2.11-124-bbbb", "v0.2.11-123-aaaa")).toBe(true);
    expect(isCliVersionNewer("v0.2.11-123-bbbb", "v0.2.11-124-aaaa")).toBe(false);
    expect(isCliVersionNewer("v0.2.11-123-bbbb", "v0.2.11-123-aaaa")).toBe(false);
  });

  it("treats a bare release as commit count zero", () => {
    expect(isCliVersionNewer("v0.2.11-1-bbbb", "v0.2.11")).toBe(true);
    expect(isCliVersionNewer("v0.2.11", "v0.2.11-1-aaaa")).toBe(false);
  });

  it("ignores invalid version strings", () => {
    expect(isCliVersionNewer("main", "v0.2.11-1-aaaa")).toBe(false);
    expect(isCliVersionNewer("v0.2.11-2-bbbb", "not-a-version")).toBe(false);
  });
});
