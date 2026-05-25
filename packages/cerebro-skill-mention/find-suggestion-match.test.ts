import { describe, expect, it } from "vitest";
import type { ResolvedPos } from "@tiptap/pm/model";
import { findSkillSuggestionMatch } from "./find-suggestion-match";

/**
 * The matcher only touches three things on the resolved position: the text
 * of the preceding text node, whether that node `isText`, and the absolute
 * position offset returned by `before()`. Mocking the surface lets each
 * case stay readable instead of dragging a full ProseMirror document in.
 */
function mockPosition(text: string | null, before = 0): ResolvedPos {
  return {
    nodeBefore:
      text === null ? null : { isText: true, text },
    before: () => before,
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

  it("computes the replacement range relative to the resolved position", () => {
    // `before()` returns the offset of the parent node before the cursor; the
    // function adds 1 to land on the first character inside that node. A
    // `/foo` at the very start of the text should rewrite `[before+1, end]`.
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/foo", 10),
    });
    expect(match?.range.from).toBe(11);
    expect(match?.range.to).toBe(15);
  });

  it("computes the range past the leading whitespace when / follows text", () => {
    // For "hi /foo" the trigger starts at index 3; with before()=10 the
    // text starts at offset 11, so `/foo` starts at 14.
    const match = findSkillSuggestionMatch({
      $position: mockPosition("hi /foo", 10),
    });
    expect(match?.range.from).toBe(14);
    expect(match?.range.to).toBe(18);
  });
});
