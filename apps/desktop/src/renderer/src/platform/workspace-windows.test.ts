// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  nextWorkspaceForNewWindow,
  resolveWindowWorkspace,
} from "./workspace-windows";

describe("resolveWindowWorkspace", () => {
  const slugs = ["work", "personal", "side"];

  it("keeps a workspace no other window shows", () => {
    expect(resolveWindowWorkspace(slugs, "personal", null, new Set(["work"]))).toBe(
      "personal",
    );
  });

  it("keeps the workspace a window was opened for even if another shows it", () => {
    expect(
      resolveWindowWorkspace(slugs, "personal", "personal", new Set(["personal"])),
    ).toBe("personal");
  });

  it("moves an unpinned window off a workspace another window shows", () => {
    expect(resolveWindowWorkspace(slugs, "work", null, new Set(["work"]))).toBe(
      "personal",
    );
  });

  it("keeps a duplicate rather than leave the window empty", () => {
    expect(
      resolveWindowWorkspace(["work"], "work", null, new Set(["work"])),
    ).toBe("work");
  });

  it("picks a free workspace when the window has none", () => {
    expect(resolveWindowWorkspace(slugs, null, null, new Set(["work"]))).toBe(
      "personal",
    );
    expect(resolveWindowWorkspace(["work"], null, null, new Set(["work"]))).toBe(
      "work",
    );
  });
});

describe("nextWorkspaceForNewWindow", () => {
  it("picks the first workspace that no window shows", () => {
    expect(
      nextWorkspaceForNewWindow(["work", "personal", "side"], "work", new Set(["personal"])),
    ).toBe("side");
  });

  it("returns null once every workspace has a window", () => {
    expect(
      nextWorkspaceForNewWindow(["work", "personal"], "work", new Set(["personal"])),
    ).toBeNull();
  });
});
