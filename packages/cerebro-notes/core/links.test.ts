import { describe, expect, it } from "vitest";
import {
  distinctNoteLinks,
  insertNoteLink,
  noteLinkHref,
  noteLinkMarkdown,
  parseNoteLinks,
  type NoteLink,
} from "./links";

const ISSUE: NoteLink = { type: "issue", id: "abc-123", label: "TECH-9 Fix login" };

describe("noteLinkMarkdown", () => {
  it("renders a markdown link with a mention href", () => {
    expect(noteLinkMarkdown(ISSUE)).toBe(
      "[TECH-9 Fix login](mention://issue/abc-123)",
    );
  });

  it("escapes brackets in the label so the link stays balanced", () => {
    expect(noteLinkMarkdown({ type: "dm", id: "d1", label: "[urgent] Anna" })).toBe(
      "[\\[urgent\\] Anna](mention://dm/d1)",
    );
  });

  it("falls back to the id when the label is empty", () => {
    expect(noteLinkMarkdown({ type: "chat", id: "s1", label: "" })).toBe(
      "[s1](mention://chat/s1)",
    );
  });
});

describe("parseNoteLinks", () => {
  it("extracts links of every supported type in order", () => {
    const body =
      "see [TECH-9](mention://issue/i1) and [#general](mention://channel/c1) " +
      "and [Anna](mention://dm/d1) and [Chat](mention://chat/s1)";
    expect(parseNoteLinks(body)).toEqual([
      { type: "issue", id: "i1", label: "TECH-9" },
      { type: "channel", id: "c1", label: "#general" },
      { type: "dm", id: "d1", label: "Anna" },
      { type: "chat", id: "s1", label: "Chat" },
    ]);
  });

  it("ignores ordinary markdown and http links", () => {
    const body = "[docs](https://x.dev) and [bare](/team/issues/1)";
    expect(parseNoteLinks(body)).toEqual([]);
  });

  it("unescapes brackets in the label", () => {
    const body = "[\\[urgent\\] Anna](mention://dm/d1)";
    expect(parseNoteLinks(body)[0]?.label).toBe("[urgent] Anna");
  });

  it("is safe on empty input", () => {
    expect(parseNoteLinks("")).toEqual([]);
  });
});

describe("distinctNoteLinks", () => {
  it("dedupes by type+id, first occurrence wins", () => {
    const body =
      "[A](mention://issue/i1) [A again](mention://issue/i1) [B](mention://channel/i1)";
    expect(distinctNoteLinks(body)).toEqual([
      { type: "issue", id: "i1", label: "A" },
      { type: "channel", id: "i1", label: "B" },
    ]);
  });
});

describe("insertNoteLink", () => {
  it("inserts at the caret with a trailing space", () => {
    const { body, caret } = insertNoteLink("hello ", 6, ISSUE);
    expect(body).toBe("hello [TECH-9 Fix login](mention://issue/abc-123) ");
    expect(caret).toBe(body.length);
  });

  it("adds a leading space when the caret is mid-word", () => {
    const { body } = insertNoteLink("hello", 5, ISSUE);
    expect(body).toBe("hello [TECH-9 Fix login](mention://issue/abc-123) ");
  });

  it("does not add a leading space at the start of the body", () => {
    const { body } = insertNoteLink("", 0, ISSUE);
    expect(body).toBe("[TECH-9 Fix login](mention://issue/abc-123) ");
  });

  it("clamps an out-of-range caret", () => {
    const { body } = insertNoteLink("abc", 99, ISSUE);
    expect(body.startsWith("abc ")).toBe(true);
  });
});

describe("noteLinkHref", () => {
  const paths = {
    issueDetail: (id: string) => `/w/issues/${id}`,
    channelDetail: (id: string) => `/w/channels/${id}`,
    inbox: () => `/w/inbox`,
  } as unknown as Parameters<typeof noteLinkHref>[0];

  it("routes each type to the right destination", () => {
    expect(noteLinkHref(paths, { type: "issue", id: "i1", label: "" })).toBe("/w/issues/i1");
    expect(noteLinkHref(paths, { type: "channel", id: "c1", label: "" })).toBe("/w/channels/c1");
    expect(noteLinkHref(paths, { type: "dm", id: "d1", label: "" })).toBe("/w/channels/d1");
    expect(noteLinkHref(paths, { type: "chat", id: "s1", label: "" })).toBe("/w/inbox?chat=s1");
  });
});
