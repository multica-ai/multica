import { describe, expect, it } from "vitest";
import { extensionDetailOptions, extensionKeys, extensionListOptions } from "./queries";

describe("platform Extension query keys", () => {
  it("keeps list and detail scoped to the workspace", () => {
    expect(extensionKeys.all("ws-1")).toEqual(["workspaces", "ws-1", "extensions"]);
    expect(extensionListOptions("ws-1").queryKey).toEqual([
      "workspaces",
      "ws-1",
      "extensions",
      "list",
    ]);
    expect(extensionDetailOptions("ws-1", "release-1").queryKey).toEqual([
      "workspaces",
      "ws-1",
      "extensions",
      "detail",
      "release-1",
    ]);
  });

  it("disables detail until both identities are available", () => {
    expect(extensionDetailOptions("", "release-1").enabled).toBe(false);
    expect(extensionDetailOptions("ws-1", "").enabled).toBe(false);
  });
});
