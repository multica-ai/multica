import { describe, it, expect } from "vitest";
import { isUpdatableOrigin } from "./origin";

describe("isUpdatableOrigin", () => {
  it("is true for URL sources with a source_url", () => {
    expect(isUpdatableOrigin({ type: "github", source_url: "https://github.com/a/b" })).toBe(true);
    expect(isUpdatableOrigin({ type: "clawhub", source_url: "https://clawhub.ai/a/b" })).toBe(true);
    expect(isUpdatableOrigin({ type: "skills_sh", source_url: "https://skills.sh/a" })).toBe(true);
  });
  it("is false without a source_url, for runtime_local, and for manual", () => {
    expect(isUpdatableOrigin({ type: "github" })).toBe(false);
    expect(isUpdatableOrigin({ type: "runtime_local", source_path: "/x" })).toBe(false);
    expect(isUpdatableOrigin({ type: "manual" })).toBe(false);
  });
});
