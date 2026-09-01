// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseActionSheet } from "./action-sheet-options";

describe("parseActionSheet", () => {
  it("lifts the cancel row out and marks the destructive action", () => {
    const parsed = parseActionSheet({
      title: "Inbox",
      options: ["Cancel", "Mark all read", "Archive all"],
      cancelButtonIndex: 0,
      destructiveButtonIndex: 2,
    });

    expect(parsed.title).toBe("Inbox");
    expect(parsed.cancel).toEqual({
      index: 0,
      label: "Cancel",
      role: "cancel",
    });
    expect(parsed.actions).toEqual([
      { index: 1, label: "Mark all read", role: "default" },
      { index: 2, label: "Archive all", role: "destructive" },
    ]);
  });

  it("keeps cancel at the end of the option list (comment long-press shape)", () => {
    const parsed = parseActionSheet({
      options: ["Reply", "Copy", "Delete", "Cancel"],
      cancelButtonIndex: 3,
      destructiveButtonIndex: 2,
    });

    expect(parsed.cancel?.index).toBe(3);
    expect(parsed.actions.map((row) => row.role)).toEqual([
      "default",
      "default",
      "destructive",
    ]);
  });
});
