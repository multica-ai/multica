import { describe, expect, it } from "vitest";

import { parseShortcutShareHash } from "./shortcut-share";

describe("parseShortcutShareHash", () => {
  it("parses a project-bound, auto-submit shortcut payload", () => {
    const result = parseShortcutShareHash(
      "#shortcut=1&workspace_id=ws-1&project_id=project-1&text=Useful%20page&url=https%3A%2F%2Fexample.com&submit=1",
    );

    expect(result).toEqual({
      title: "",
      text: "Useful page",
      url: "https://example.com",
      workspaceId: "ws-1",
      projectId: "project-1",
      autoSubmit: true,
    });
  });

  it("rejects payloads without a fixed destination", () => {
    expect(parseShortcutShareHash("#shortcut=1&text=No%20destination")).toBeNull();
  });

  it("ignores unrelated URL fragments", () => {
    expect(parseShortcutShareHash("#section=comments")).toBeNull();
  });

  it("rejects content too large for a reliable iOS app-launch URL", () => {
    expect(
      parseShortcutShareHash(
        `#shortcut=1&workspace_id=ws-1&project_id=project-1&text=${"x".repeat(6_001)}`,
      ),
    ).toBeNull();
  });
});
