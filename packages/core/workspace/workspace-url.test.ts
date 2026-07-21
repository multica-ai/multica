import { describe, expect, it } from "vitest";
import { workspaceUrlHost } from "./workspace-url";

describe("workspaceUrlHost", () => {
  it("returns the host of a full app URL", () => {
    expect(workspaceUrlHost("https://multica.example.com")).toBe(
      "multica.example.com",
    );
  });

  it("ignores scheme, path, and trailing slash", () => {
    expect(workspaceUrlHost("https://multica.example.com/")).toBe(
      "multica.example.com",
    );
    expect(workspaceUrlHost("http://multica.example.com/app/onboarding")).toBe(
      "multica.example.com",
    );
  });

  it("preserves a non-default port", () => {
    expect(workspaceUrlHost("https://my.host:3000")).toBe("my.host:3000");
  });

  it("accepts a bare host without a scheme", () => {
    expect(workspaceUrlHost("multica.example.com")).toBe("multica.example.com");
    expect(workspaceUrlHost("multica.example.com/path")).toBe(
      "multica.example.com",
    );
  });

  it("falls back to the brand host when no app URL is configured", () => {
    expect(workspaceUrlHost("")).toBe("multica.ai");
    expect(workspaceUrlHost("   ")).toBe("multica.ai");
    expect(workspaceUrlHost(null)).toBe("multica.ai");
    expect(workspaceUrlHost(undefined)).toBe("multica.ai");
  });
});
