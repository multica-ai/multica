// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useCommentComposerStore } from "./comment-composer-store";

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.setItem !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

describe("comment composer store", () => {
  beforeEach(() => {
    useCommentComposerStore.setState({ sticky: true });
  });

  it("defaults to sticky", () => {
    expect(useCommentComposerStore.getState().sticky).toBe(true);
  });

  it("toggleSticky flips the preference", () => {
    useCommentComposerStore.getState().toggleSticky();
    expect(useCommentComposerStore.getState().sticky).toBe(false);

    useCommentComposerStore.getState().toggleSticky();
    expect(useCommentComposerStore.getState().sticky).toBe(true);
  });
});
