import { describe, expect, it } from "vitest";
import { adfToText } from "./adf";

describe("adfToText", () => {
  it("returns a plain string unchanged", () => {
    expect(adfToText("hello")).toBe("hello");
  });

  it("returns empty string for null/undefined", () => {
    expect(adfToText(null)).toBe("");
    expect(adfToText(undefined)).toBe("");
  });

  it("extracts text from a paragraph doc", () => {
    const doc = {
      type: "doc",
      content: [
        { type: "paragraph", content: [{ type: "text", text: "Line one" }] },
        { type: "paragraph", content: [{ type: "text", text: "Line two" }] },
      ],
    };
    expect(adfToText(doc)).toBe("Line one\n\nLine two");
  });

  it("renders bullet list items", () => {
    const doc = {
      type: "doc",
      content: [
        {
          type: "bulletList",
          content: [
            { type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "a" }] }] },
            { type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "b" }] }] },
          ],
        },
      ],
    };
    expect(adfToText(doc)).toBe("- a\n- b");
  });

  it("ignores unknown node types but keeps their text children", () => {
    const doc = { type: "doc", content: [{ type: "weird", content: [{ type: "text", text: "kept" }] }] };
    expect(adfToText(doc)).toBe("kept");
  });
});
