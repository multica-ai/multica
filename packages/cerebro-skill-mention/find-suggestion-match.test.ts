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
  it("matches /skill at the start of the text node", () => {
    const match = findSkillSuggestionMatch({ $position: mockPosition("/skill") });
    expect(match).not.toBeNull();
    expect(match?.query).toBe("");
    expect(match?.text).toBe("/skill");
  });

  it("matches /skill after whitespace", () => {
    const match = findSkillSuggestionMatch({ $position: mockPosition("hi /skill") });
    expect(match).not.toBeNull();
    expect(match?.query).toBe("");
    expect(match?.text).toBe("/skill");
  });

  it("does not match /skill mid-token (path-like prefix)", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("apps/skills/foo") }),
    ).toBeNull();
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("app/skill") }),
    ).toBeNull();
  });

  it("matches case-insensitively", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/Skill") })?.text,
    ).toBe("/Skill");
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/SKILL") })?.text,
    ).toBe("/SKILL");
  });

  it("does not match /skills (extra letter — word boundary)", () => {
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/skills") }),
    ).toBeNull();
    expect(
      findSkillSuggestionMatch({ $position: mockPosition("/skillz") }),
    ).toBeNull();
  });

  it("extracts the query after a single whitespace separator", () => {
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/skill foo"),
    });
    expect(match?.query).toBe("foo");
    expect(match?.text).toBe("/skill foo");
  });

  it("preserves multi-word queries", () => {
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/skill foo bar"),
    });
    expect(match?.query).toBe("foo bar");
  });

  it("trims leading whitespace inside the query", () => {
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/skill   foo"),
    });
    expect(match?.query).toBe("foo");
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
    // /skill at the very start of the text should rewrite `[before+1, end]`.
    const match = findSkillSuggestionMatch({
      $position: mockPosition("/skill foo", 10),
    });
    expect(match?.range.from).toBe(11);
    expect(match?.range.to).toBe(21);
  });

  it("computes the range past the leading whitespace when /skill follows text", () => {
    // For "hi /skill" the trigger starts at index 3; with before()=10 the
    // text starts at offset 11, so `/skill` starts at 14.
    const match = findSkillSuggestionMatch({
      $position: mockPosition("hi /skill", 10),
    });
    expect(match?.range.from).toBe(14);
    expect(match?.range.to).toBe(20);
  });
});
