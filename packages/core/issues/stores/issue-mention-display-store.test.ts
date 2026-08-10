// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useIssueMentionDisplayStore } from "./issue-mention-display-store";

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.setItem !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() {
        return values.size;
      },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => {
        values.delete(k);
      },
      setItem: (k, v) => {
        values.set(k, v);
      },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

describe("issue mention display store", () => {
  beforeEach(() => {
    useIssueMentionDisplayStore.setState({ mode: "full" });
  });

  it("defaults to the full chip so nothing changes for people who never opt in", () => {
    expect(useIssueMentionDisplayStore.getState().mode).toBe("full");
  });

  it("stores the selected mode", () => {
    useIssueMentionDisplayStore.getState().setMode("compact");
    expect(useIssueMentionDisplayStore.getState().mode).toBe("compact");

    useIssueMentionDisplayStore.getState().setMode("plain");
    expect(useIssueMentionDisplayStore.getState().mode).toBe("plain");
  });
});
