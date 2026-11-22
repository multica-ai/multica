import { renderHook } from "@testing-library/react";
import { useLayoutEffect } from "react";
import { describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { locateCaret, useDocCollab } from "./use-doc-collab";
import type { DocSession } from "./doc-live-session";

vi.mock("@multica/core/realtime", () => ({ useWS: () => ({ collabUrl: "ws://collab" }) }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: unknown) => unknown) => select({ user: { id: "me", name: "Me", email: "me@x" } }),
}));
vi.mock("./doc-live-session", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./doc-live-session")>();
  const state = { ready: true, synced: true, connected: true, collaborators: [], reset: false };
  let id = 0;
  return {
    ...actual,
    peekDocLive: (_url: string, docId: string) => ({ enabled: true, document: `issue:${docId}` }),
    findDocSession: () => undefined,
    acquireDocSession: (_url: string, _docId: string, _user: string, document: string) => ({
      id: ++id,
      document,
      ydoc: new Y.Doc(),
      provider: {},
      subscribe: () => () => {},
      getState: () => state,
      watchAwareness: () => () => {},
    }),
    releaseDocSession: () => {},
  };
});

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

describe("useDocCollab", () => {
  it("never hands the next doc the previous doc's session", () => {
    const seen: { docId: string; generation: number | null }[] = [];
    const { rerender } = renderHook(
      ({ docId }) => {
        const state = useDocCollab(docId, "", "issue");
        const generation = state.mode === "live" ? state.generation : null;
        useLayoutEffect(() => {
          seen.push({ docId, generation });
        });
      },
      { initialProps: { docId: "a" } },
    );
    const first = seen.at(-1)?.generation;
    expect(first).not.toBeNull();

    rerender({ docId: "b" });
    const afterSwitch = seen.slice(seen.findIndex((s) => s.docId === "b"));
    expect(afterSwitch.some((s) => s.generation === first)).toBe(false);
    expect(afterSwitch.at(-1)?.generation).not.toBeNull();
  });
});
