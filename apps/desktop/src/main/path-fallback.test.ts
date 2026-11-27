// @vitest-environment node
import { describe, expect, it } from "vitest";
import { appendMissingPathDirs } from "./path-fallback";

describe("appendMissingPathDirs", () => {
  const fallbacks = [
    "/opt/homebrew/bin",
    "/usr/local/bin",
    "/Users/me/.local/bin",
  ];

  it("appends only dirs that are not already present", () => {
    const current = "/Users/me/.nvm/versions/node/v22.0.0/bin:/usr/bin";
    expect(appendMissingPathDirs(current, fallbacks)).toBe(
      [
        "/Users/me/.nvm/versions/node/v22.0.0/bin",
        "/usr/bin",
        "/opt/homebrew/bin",
        "/usr/local/bin",
        "/Users/me/.local/bin",
      ].join(":"),
    );
  });

  it("leaves PATH unchanged when every fallback is already present", () => {
    const current = [
      "/opt/homebrew/bin",
      "/usr/local/bin",
      "/Users/me/.local/bin",
      "/usr/bin",
    ].join(":");
    expect(appendMissingPathDirs(current, fallbacks)).toBe(current);
  });

  it("still adds every fallback when PATH is empty", () => {
    expect(appendMissingPathDirs("", fallbacks)).toBe(fallbacks.join(":"));
  });

  it("never prepends — recovered nvm Node stays ahead of /usr/local/bin", () => {
    const current = "/Users/me/.nvm/versions/node/v22.0.0/bin";
    const next = appendMissingPathDirs(current, ["/usr/local/bin"]);
    expect(next.startsWith("/Users/me/.nvm/versions/node/v22.0.0/bin")).toBe(
      true,
    );
    expect(next.endsWith("/usr/local/bin")).toBe(true);
  });
});
