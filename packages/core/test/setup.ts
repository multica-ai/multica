// Node 25 exposes a global `localStorage` object even when no
// --localstorage-file is configured, but that placeholder has none of the
// Storage methods. It shadows jsdom's implementation and breaks tests that
// correctly exercise browser persistence. Install a small standards-shaped
// in-memory implementation only for that invalid runtime state.
if (typeof globalThis.localStorage?.getItem !== "function") {
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      get length() {
        return values.size;
      },
      clear() {
        values.clear();
      },
      getItem(key: string) {
        return values.get(String(key)) ?? null;
      },
      key(index: number) {
        return [...values.keys()][index] ?? null;
      },
      removeItem(key: string) {
        values.delete(String(key));
      },
      setItem(key: string, value: string) {
        values.set(String(key), String(value));
      },
    },
  });
}
