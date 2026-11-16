// Node 25+ enables Web Storage by default. Without a valid
// `--localstorage-file` its global `localStorage` has no methods, and it
// shadows the working one jsdom provides. Swap in an in-memory Storage so jsdom
// suites behave the same on every Node version `engines` allows.

function createMemoryStorage(): Storage {
  const values = new Map<string, string>();

  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => Array.from(values.keys())[index] ?? null,
    removeItem: (key: string) => {
      values.delete(key);
    },
    setItem: (key: string, value: string) => {
      values.set(key, String(value));
    },
  };
}

// Suites on the node environment have no DOM storage to stand in for.
if (typeof window !== "undefined") {
  const localStorageIsUsable =
    typeof globalThis.localStorage?.getItem === "function" &&
    typeof globalThis.localStorage?.setItem === "function" &&
    typeof globalThis.localStorage?.removeItem === "function" &&
    typeof globalThis.localStorage?.clear === "function";

  if (!localStorageIsUsable) {
    const storage = createMemoryStorage();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
}

export {};
