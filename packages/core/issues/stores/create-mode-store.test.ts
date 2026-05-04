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

  it("initializes missing persisted storage when reading the create mode", async () => {
    const { getPersistedCreateMode } = await import("./create-mode-store");

    expect(localStorage.getItem("multica_create_mode")).toBeNull();
    expect(getPersistedCreateMode()).toBe("manual");
    expect(localStorage.getItem("multica_create_mode")).toBe(
      JSON.stringify({ state: { lastMode: "manual" }, version: 0 }),
    );
  });

  it("uses an existing persisted create mode without overwriting it", async () => {
    localStorage.setItem(
      "multica_create_mode",
      JSON.stringify({ state: { lastMode: "agent" }, version: 0 }),
    );

    const { getPersistedCreateMode } = await import("./create-mode-store");

    expect(getPersistedCreateMode()).toBe("agent");
    expect(localStorage.getItem("multica_create_mode")).toBe(
      JSON.stringify({ state: { lastMode: "agent" }, version: 0 }),
    );
  });
});
