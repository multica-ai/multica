// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useOpenLocalFileStore } from "./open-local-file-store";

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
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
});

describe("open local file store", () => {
  beforeEach(() => {
    useOpenLocalFileStore.setState({ openerId: null });
  });

  it("starts unconfigured so the desktop shell can apply Cursor → VS Code → file manager", () => {
    expect(useOpenLocalFileStore.getState().openerId).toBeNull();
  });

  it("remembers the chosen opener on this device", () => {
    useOpenLocalFileStore.getState().setOpenerId("vscode");
    expect(useOpenLocalFileStore.getState().openerId).toBe("vscode");
  });
});
