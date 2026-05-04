import { beforeEach, describe, expect, it, vi } from "vitest";

describe("useCreateModeStore", () => {
  beforeEach(() => {
    vi.resetModules();
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => {
        storage.set(key, value);
      },
      removeItem: (key: string) => {
        storage.delete(key);
      },
      clear: () => {
        storage.clear();
      },
    });
    vi.stubGlobal("window", { localStorage });
  });

  it("defaults to manual mode for first-time issue creation", async () => {
    const { useCreateModeStore } = await import("./create-mode-store");

    expect(useCreateModeStore.getState().lastMode).toBe("manual");
  });
});
