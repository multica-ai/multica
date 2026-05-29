import { describe, expect, it } from "vitest";
import type { ResolvedPos } from "@tiptap/pm/model";
import { findSkillSuggestionMatch } from "./find-suggestion-match";

/**
 * The matcher only touches three things on the resolved position: the text
 * of the preceding text node, whether that node `isText`, and the absolute
 * cursor position `pos`. Mocking the surface lets each case stay readable
 * instead of dragging a full ProseMirror document in.
 */
function mockPosition(text: string | null, pos = 0): ResolvedPos {
  return {
    nodeBefore:
      text === null ? null : { isText: true, text },
    pos,
  } as unknown as ResolvedPos;
}

describe("findSkillSuggestionMatch", () => {
  it("matches a bare / at the start of the text node (empty query)", () => {
    const match = findSkillSuggestionMatch({ $position: mockPosition("/") });
    expect(match).not.toBeNull();
    expect(match?.query).toBe("");
    expect(match?.text).toBe("/");
  });

  it("matches a bare / after whitespace", () => {
    const match = findSkillSuggestionMatch({ $position: mockPosition("hi /") });
    expect(match).not.toBeNull();
    expect(match?.query).toBe("");
    expect(match?.text).toBe("/");
  });

  it("filters by whatever follows / (slash-command UX)", () => {
    const match = findSkillSuggestionMatch({ $position: mockPosition("/foo") });
    expect(match?.query).toBe("foo");
    expect(match?.text).toBe("/foo");
  });

  it("supports filtering by skill-name substring", () => {
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/agent"),
    });
    expect(match?.query).toBe("agent");
  });

  it("legacy /skill input still matches (skill as the filter query)", () => {
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/skill"),
    });
    expect(match?.query).toBe("skill");
  });

  it("does not match / mid-token (paths)", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("apps/skills/foo") }),
    ).toBeNull();
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("app/skill") }),
    ).toBeNull();
  });

  it("does not match inside a URL", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("https://example") }),
    ).toBeNull();
  });

  it("typing a space after the query closes the popover", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/foo ") }),
    ).toBeNull();
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/foo bar") }),
    ).toBeNull();
  });

  it("typing a second / closes the popover", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/foo/bar") }),
    ).toBeNull();
  });

  it("returns null when the node before the cursor is not a text node", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition(null) }),
    ).toBeNull();
  });

  it("returns null on empty text", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("") }),
    ).toBeNull();
  });

  it("computes the replacement range relative to the cursor position", () => {
    // The range is anchored to the cursor: `nodeBefore.text` ("/foo", len 4)
    // ends at pos=15, so it starts at 11. `/foo` therefore rewrites [11, 15].
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/foo", 15),
    });
    expect(match?.range.from).toBe(11);
    expect(match?.range.to).toBe(15);
  });

  it("computes the range past the leading whitespace when / follows text", () => {
    // For "hi /foo" (len 7) ending at pos=18, the text starts at 11; the
    // trigger sits after the leading space, so `/foo` starts at 14.
    const match = findSkillSuggestionMatch({
      $position: mockPosition("hi /foo", 18),
    });
    expect(match?.range.from).toBe(14);
    expect(match?.range.to).toBe(18);
  });

  it("anchors the range to the cursor even when the text node starts mid-paragraph (FIR-2299)", () => {
    // Regression: when earlier inline content (another mention chip, a hard
    // break) pushes the text node deep into the paragraph, the trigger text
    // "/foo" might end at pos=53. Cursor-relative math keeps the range exactly
    // on the typed `/foo` ([49, 53]); the old paragraph-relative math produced
    // a too-small offset and inserted the skill at a random earlier position.
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/foo", 53),
    });
    expect(match?.range.from).toBe(49);
    expect(match?.range.to).toBe(53);
  });
});
