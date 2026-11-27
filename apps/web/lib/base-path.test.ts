// @vitest-environment node
import { describe, expect, it } from "vitest";
import { withBasePath } from "./base-path";

describe("withBasePath", () => {
  it.each([
    ["/favicon.svg", "", "/favicon.svg"],
    ["/favicon.svg", "/multica", "/multica/favicon.svg"],
    ["/icons/icon-192.png", "/multica/", "/multica/icons/icon-192.png"],
    ["/multica/favicon.svg", "/multica", "/multica/favicon.svg"],
    ["https://cdn.example.com/x.png", "/multica", "https://cdn.example.com/x.png"],
  ])("%s with base %j -> %s", (path, base, want) => {
    expect(withBasePath(path, base)).toBe(want);
  });
});
