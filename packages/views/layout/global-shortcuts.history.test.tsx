import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureShortcutPlatform, createShortcutChord, useShortcutStore } from "@multica/core/shortcuts";
import { useWorkspaceSwitcherStore } from "@multica/core/workspace/switcher-store";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { GlobalShortcuts } from "./global-shortcuts";

// GlobalShortcuts pulls workspace paths and the sidebar/chat stores at render
// time; none of that is exercised by the history chords, so stub them to keep
// the test focused on the back/forward wiring and free of provider setup.
const sidebar = vi.hoisted(() => ({ toggleSidebar: vi.fn(), setOpen: vi.fn(), setOpenMobile: vi.fn(), isCompact: false }));
vi.mock("@multica/ui/components/ui/sidebar", () => ({ useSidebar: () => sidebar }));
vi.mock("@multica/core/chat", () => ({
  useChatStore: { getState: () => ({ floatingChatEnabled: false }) },
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/w/inbox",
    chat: () => "/w/chat",
    myIssues: () => "/w/my-issues",
    issues: () => "/w/issues",
    projects: () => "/w/projects",
    autopilots: () => "/w/autopilots",
    agents: () => "/w/agents",
    squads: () => "/w/squads",
    usage: () => "/w/usage",
    runtimes: () => "/w/runtimes",
    skills: () => "/w/skills",
    settings: () => "/w/settings",
  }),
}));

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    forward: vi.fn(),
    pathname: "/w/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
    ...overrides,
  };
}

function renderShortcuts(adapter: NavigationAdapter) {
  return render(
    <NavigationProvider value={adapter}>
      <GlobalShortcuts />
    </NavigationProvider>,
  );
}

function pressBracket(key: "[" | "]", target?: EventTarget) {
  const event = new KeyboardEvent("keydown", {
    key,
    metaKey: true,
    bubbles: true,
    cancelable: true,
  });
  (target ?? document).dispatchEvent(event);
  return event;
}

describe("GlobalShortcuts history navigation", () => {
  beforeEach(() => {
    // Pin the platform so Cmd (metaKey) maps to the primary modifier, matching
    // the Mod+[ / Mod+] defaults regardless of the host running the test.
    configureShortcutPlatform("macos");
  });

  afterEach(() => {
    cleanup();
    configureShortcutPlatform(null);
    useWorkspaceSwitcherStore.getState().setOpen(false);
    useShortcutStore.getState().resetAll();
  });

  it("toggles the workspace switcher from an editable control and reveals its sidebar", () => {
    const { unmount } = renderShortcuts(makeAdapter());
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    const press = () => input.dispatchEvent(new KeyboardEvent("keydown", {
      key: "O", metaKey: true, shiftKey: true, bubbles: true, cancelable: true,
    }));
    act(press);
    expect(useWorkspaceSwitcherStore.getState().isOpen).toBe(true);
    expect(sidebar.setOpen).toHaveBeenCalledWith(true);
    act(press);
    expect(useWorkspaceSwitcherStore.getState().isOpen).toBe(false);
    input.remove();
    unmount();
  });

  it("uses the customized workspace switcher binding immediately", () => {
    const { unmount } = renderShortcuts(makeAdapter());
    act(() => useShortcutStore.getState().setShortcut(
      "toggleWorkspaceSwitcher", createShortcutChord("G", { primary: true, shift: true }),
    ));
    act(() => document.dispatchEvent(new KeyboardEvent("keydown", {
      key: "O", metaKey: true, shiftKey: true, bubbles: true,
    })));
    expect(useWorkspaceSwitcherStore.getState().isOpen).toBe(false);
    act(() => document.dispatchEvent(new KeyboardEvent("keydown", {
      key: "G", metaKey: true, shiftKey: true, bubbles: true,
    })));
    expect(useWorkspaceSwitcherStore.getState().isOpen).toBe(true);
    unmount();
  });

  it("steps back through history on Mod+[", () => {
    const adapter = makeAdapter();
    const { unmount } = renderShortcuts(adapter);

    const event = pressBracket("[");

    expect(adapter.back).toHaveBeenCalledTimes(1);
    expect(adapter.forward).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(true);
    unmount();
  });

  it("steps forward through history on Mod+]", () => {
    const adapter = makeAdapter();
    const { unmount } = renderShortcuts(adapter);

    const event = pressBracket("]");

    expect(adapter.forward).toHaveBeenCalledTimes(1);
    expect(adapter.back).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(true);
    unmount();
  });

  it("stays a no-op on an adapter without a forward stack", () => {
    const adapter = makeAdapter({ forward: undefined });
    const { unmount } = renderShortcuts(adapter);

    // Must not throw even though forward() is undefined on this adapter.
    expect(() => pressBracket("]")).not.toThrow();
    expect(adapter.back).not.toHaveBeenCalled();
    unmount();
  });

  it("ignores the chord while focus is inside an editable field", () => {
    const adapter = makeAdapter();
    const { unmount } = renderShortcuts(adapter);

    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    pressBracket("[", input);

    expect(adapter.back).not.toHaveBeenCalled();
    input.remove();
    unmount();
  });
});
