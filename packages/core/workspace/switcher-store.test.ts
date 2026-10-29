// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { useWorkspaceSwitcherStore } from "./switcher-store";

afterEach(() => useWorkspaceSwitcherStore.getState().setOpen(false));

describe("workspace switcher shortcut requests", () => {
  it("only claims numbered selection while the menu is open", () => {
    const store = useWorkspaceSwitcherStore.getState();
    expect(store.requestSelection(1)).toBe(false);
    expect(useWorkspaceSwitcherStore.getState().requestedIndex).toBeNull();
    store.setOpen(true);
    expect(store.requestSelection(1)).toBe(true);
    expect(useWorkspaceSwitcherStore.getState().requestedIndex).toBe(1);
  });

  it("clears a pending selection when dismissed so reopening cannot switch workspaces", () => {
    const store = useWorkspaceSwitcherStore.getState();
    store.setOpen(true);
    store.requestSelection(2);
    store.setOpen(false);
    store.toggle();
    expect(useWorkspaceSwitcherStore.getState()).toMatchObject({
      isOpen: true,
      requestedIndex: null,
    });
  });
});
