import { describe, expect, it } from "vitest";
import { compareVersions } from "./compare";

describe("compareVersions", () => {
  it("flags app_newer when the app is ahead on major or minor", () => {
    expect(compareVersions("0.9.0", "0.8.2")).toBe("app_newer");
    expect(compareVersions("1.0.0", "0.9.9")).toBe("app_newer");
    expect(compareVersions("0.9.0", "0.8.9")).toBe("app_newer");
  });

  it("flags app_newer on patch drift too (the #5848 scenario)", () => {
    expect(compareVersions("0.4.9", "0.4.8")).toBe("app_newer");
    expect(compareVersions("0.8.2", "0.8.0")).toBe("app_newer");
  });

  it("flags server_newer when the server is ahead on any level", () => {
    expect(compareVersions("0.8.0", "0.9.0")).toBe("server_newer");
    expect(compareVersions("0.9.9", "1.0.0")).toBe("server_newer");
    expect(compareVersions("0.4.8", "0.4.9")).toBe("server_newer");
    expect(compareVersions("0.8.0", "0.8.2")).toBe("server_newer");
  });

  it("treats identical versions as equal", () => {
    expect(compareVersions("1.2.3", "1.2.3")).toBe("equal");
    expect(compareVersions("0.9", "0.9.0")).toBe("equal");
  });

  it("accepts a leading v and prerelease/build suffixes", () => {
    expect(compareVersions("v0.9.0", "0.8.0")).toBe("app_newer");
    expect(compareVersions("0.9.0-beta.1", "0.8.0")).toBe("app_newer");
    expect(compareVersions("0.9.0+build.7", "0.9.1")).toBe("server_newer");
  });

  it("returns unknown when either side is missing", () => {
    // Cloud /api/config omits server_version entirely.
    expect(compareVersions("0.9.0", undefined)).toBe("unknown");
    expect(compareVersions("0.9.0", "")).toBe("unknown");
    expect(compareVersions(undefined, "0.9.0")).toBe("unknown");
    expect(compareVersions(null, null)).toBe("unknown");
  });

  it("returns unknown for non-semver strings", () => {
    expect(compareVersions("dev", "0.9.0")).toBe("unknown");
    expect(compareVersions("0.9.0", "unknown")).toBe("unknown");
    expect(compareVersions("0.9.0", "dev")).toBe("unknown");
    expect(compareVersions("0.9.0", "a1b2c3d")).toBe("unknown");
  });

  it("treats the 0.1.0 dev placeholder as unknown, not a real version", () => {
    expect(compareVersions("0.1.0", "0.9.0")).toBe("unknown");
    expect(compareVersions("0.9.0", "0.1.0")).toBe("unknown");
    expect(compareVersions("0.1.0", "0.1.0")).toBe("unknown");
  });
});
