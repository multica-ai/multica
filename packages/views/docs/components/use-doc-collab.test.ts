import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { locateCaret } from "./use-doc-collab";
import type { DocSession } from "./doc-live-session";

function block(name: string, text: string, attrs: Record<string, string> = {}) {
  const element = new Y.XmlElement(name);
  for (const [key, value] of Object.entries(attrs)) element.setAttribute(key, value);
  const content = new Y.XmlText();
  content.insert(0, text);
  element.insert(0, [content]);
  return element;
}

/** A session whose one collaborator (key 7) has a caret `offset` characters into block `index`. */
function sessionWithCaret(blocks: Y.XmlElement[], index: number | null, offset = 0) {
  const ydoc = new Y.Doc();
  const root = ydoc.getXmlFragment("default");
  root.insert(0, blocks);
  const states = new Map<number, unknown>();
  if (index !== null) {
    const text = (root.get(index) as Y.XmlElement).get(0) as Y.XmlText;
    const head = Y.relativePositionToJSON(Y.createRelativePositionFromTypeIndex(text, offset));
    states.set(7, { user: { id: "u", name: "U" }, cursor: { anchor: head, head } });
  } else {
    states.set(7, { user: { id: "u", name: "U" } });
  }
  return { ydoc, provider: { awareness: { getStates: () => states } } } as unknown as DocSession;
}

describe("locateCaret", () => {
  const doc = () => [
    block("paragraph", "Intro before any heading"),
    block("heading", "Goals", { level: "2" }),
    block("paragraph", "Ship by Q4"),
    block("heading", "Risks", { level: "2" }),
    block("paragraph", "The import API"),
  ];

  it("names the heading above the caret", () => {
    expect(locateCaret(sessionWithCaret(doc(), 2, 3), 7)).toBe("Goals");
    expect(locateCaret(sessionWithCaret(doc(), 4, 0), 7)).toBe("Risks");
  });

  it("names the heading the caret is in", () => {
    expect(locateCaret(sessionWithCaret(doc(), 3, 2), 7)).toBe("Risks");
  });

  it("says nothing above the first heading, without a caret, or for someone unknown", () => {
    expect(locateCaret(sessionWithCaret(doc(), 0, 1), 7)).toBeNull();
    expect(locateCaret(sessionWithCaret(doc(), null), 7)).toBeNull();
    expect(locateCaret(sessionWithCaret(doc(), 2), 8)).toBeNull();
  });
});
