import { describe, expect, it } from "vitest";
import {
  DEFAULT_EDITOR_TOOLBAR_ORDER,
  EDITOR_TOOLBAR_LIST_TYPE_KEY,
  readEditorToolbarRow,
} from "./editor-toolbar-preferences";

const rest = (...kept: string[]) =>
  DEFAULT_EDITOR_TOOLBAR_ORDER.filter(
    (action) => !kept.includes(action),
  ) as string[];

describe("the toolbar action contract", () => {
  it("contains only the new ids — the three list buttons and both indent buttons are gone", () => {
    expect([...DEFAULT_EDITOR_TOOLBAR_ORDER]).toEqual([
      "heading",
      "bold",
      "italic",
      "strike",
      "highlight",
      "code",
      "link",
      "lists",
      "blockquote",
      "comment",
    ]);
  });
});

describe("readEditorToolbarRow", () => {
  it("still loads a bare array saved by the old build, mapping the removed ids", () => {
    expect(
      readEditorToolbarRow([
        "orderedList",
        "bold",
        "bulletList",
        "taskList",
        "indent",
        "outdent",
      ]),
    ).toEqual({
      order: ["lists", "bold", ...rest("lists", "bold")],
      hidden: [],
    });
  });

  it("accepts the object shape and normalizes duplicates and unknown ids", () => {
    expect(
      readEditorToolbarRow({
        order: ["comment", "lists", "comment", "nonsense"],
        hidden: ["blockquote", "blockquote", "taskList"],
      }),
    ).toEqual({
      order: ["comment", "lists", ...rest("comment", "lists")],
      hidden: ["blockquote", "lists"],
    });
  });

  it("keeps a user's order and appends any action added since they saved", () => {
    expect(readEditorToolbarRow(["italic", "bold"]).order).toEqual([
      "italic",
      "bold",
      ...rest("italic", "bold"),
    ]);
  });

  it.each([null, "bold", 42, { order: ["unknown"], hidden: "nope" }])(
    "falls back to the default row without throwing for %j",
    (value) => {
      expect(readEditorToolbarRow(value)).toEqual({
        order: [...DEFAULT_EDITOR_TOOLBAR_ORDER],
        hidden: [],
      });
    },
  );

  it("declares the list-type preference key", () => {
    expect(EDITOR_TOOLBAR_LIST_TYPE_KEY).toBe("cerebro_editor_list_type");
  });
});
