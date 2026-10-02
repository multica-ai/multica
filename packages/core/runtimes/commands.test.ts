// @vitest-environment node
import { describe, expect, it } from "vitest";
import { daemonCommands, normalizeCommandURL } from "./commands";

describe("normalizeCommandURL", () => {
  it("trims and strips trailing slashes", () => {
    expect(normalizeCommandURL("  https://api.example.com/// ")).toBe(
      "https://api.example.com",
    );
    expect(normalizeCommandURL("")).toBe("");
    expect(normalizeCommandURL(undefined)).toBe("");
  });
});

describe("daemonCommands", () => {
  it("returns cloud setup and token commands by default", () => {
    const { setupCmd, tokenCmd } = daemonCommands(undefined, undefined);
    expect(setupCmd).toBe("multica setup");
    expect(tokenCmd).toContain("https://api.multica.ai");
    expect(tokenCmd).toContain("https://multica.ai");
  });

  it("returns cloud setup when only one URL is configured", () => {
    const { setupCmd } = daemonCommands("https://api.example.com", undefined);
    expect(setupCmd).toBe("multica setup");
  });

  it("returns self-host setup command when both URLs are provided", () => {
    const { setupCmd, tokenCmd } = daemonCommands(
      "https://api.example.com/",
      "https://app.example.com/",
    );
    expect(setupCmd).toBe(
      "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
    expect(tokenCmd).toContain(
      "multica config set server_url https://api.example.com",
    );
    expect(tokenCmd).toContain(
      "multica config set app_url https://app.example.com",
    );
  });
});
