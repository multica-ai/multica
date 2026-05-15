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

describe("openCreateIssueWithPreference", () => {
  const initialMode = "manual" as const;

  beforeEach(async () => {
    const { useModalStore } = await import("../../modals");
    useModalStore.getState().close();
  });

  it("opens quick-create-issue when last mode is agent", async () => {
    const { openCreateIssueWithPreference, useCreateModeStore } = await import("./create-mode-store");
    const { useModalStore } = await import("../../modals");
    useCreateModeStore.getState().setLastMode("agent");
    openCreateIssueWithPreference();
    expect(useModalStore.getState().modal).toBe("quick-create-issue");
    expect(useModalStore.getState().data).toBeNull();
  });

  it("opens create-issue when last mode is manual", async () => {
    const { openCreateIssueWithPreference, useCreateModeStore } = await import("./create-mode-store");
    const { useModalStore } = await import("../../modals");
    useCreateModeStore.getState().setLastMode("manual");
    openCreateIssueWithPreference();
    expect(useModalStore.getState().modal).toBe("create-issue");
  });

  it("forwards seed data to whichever modal is opened", async () => {
    const { openCreateIssueWithPreference, useCreateModeStore } = await import("./create-mode-store");
    const { useModalStore } = await import("../../modals");
    useCreateModeStore.getState().setLastMode("manual");
    openCreateIssueWithPreference({ project_id: "p1" });
    expect(useModalStore.getState().modal).toBe("create-issue");
    expect(useModalStore.getState().data).toEqual({ project_id: "p1" });

    useCreateModeStore.getState().setLastMode("agent");
    openCreateIssueWithPreference({ project_id: "p2" });
    expect(useModalStore.getState().modal).toBe("quick-create-issue");
    expect(useModalStore.getState().data).toEqual({ project_id: "p2" });

    useCreateModeStore.getState().setLastMode(initialMode);
    useModalStore.getState().close();
  });
});
