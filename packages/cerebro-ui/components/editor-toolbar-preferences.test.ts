import { describe, expect, it } from "vitest";
import {
  DEFAULT_EDITOR_TOOLBAR_ORDER,
  readEditorToolbarOrder,
} from "./editor-toolbar-preferences";

describe("readEditorToolbarOrder", () => {
  it("keeps a user's valid order, removes duplicates, and appends newly added actions", () => {
    expect(
      readEditorToolbarOrder(["italic", "bold", "italic", "unknown"]),
    ).toEqual([
      "italic",
      "bold",
      ...DEFAULT_EDITOR_TOOLBAR_ORDER.filter(
        (action) => action !== "italic" && action !== "bold",
      ),
    ]);
  });

  it("uses the complete default toolbar when the saved preference is invalid", () => {
    expect(readEditorToolbarOrder("bold")).toEqual(
      DEFAULT_EDITOR_TOOLBAR_ORDER,
    );
  });
});
