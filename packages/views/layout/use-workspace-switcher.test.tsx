import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureShortcutPlatform } from "@multica/core/shortcuts";
import { useWorkspaceSwitcherStore } from "@multica/core/workspace/switcher-store";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { useWorkspaceSwitcher } from "./use-workspace-switcher";

const workspaces = [
  { id: "one", slug: "alpha" },
  { id: "two", slug: "beta" },
  { id: "three", slug: "gamma" },
];

function setup(activeId = "one") {
  const push = vi.fn();
  const adapter: NavigationAdapter = {
    push, replace: vi.fn(), back: vi.fn(), pathname: "/alpha/issues/current",
    searchParams: new URLSearchParams(), hash: "", getShareableUrl: (path) => path,
  };
  return {
    push,
    ...renderHook(({ currentId }) => useWorkspaceSwitcher(workspaces, currentId), {
      initialProps: { currentId: activeId },
      wrapper: ({ children }) => <NavigationProvider value={adapter}>{children}</NavigationProvider>,
    }),
  };
}

beforeEach(() => configureShortcutPlatform("macos"));
afterEach(() => {
  cleanup();
  useWorkspaceSwitcherStore.getState().setOpen(false);
  useWorkspaceSwitcherStore.getState().setPrimaryHeld(false);
  configureShortcutPlatform(null);
});

describe("workspace switcher keyboard navigation", () => {
  it("selects a numbered workspace through the navigation adapter and closes", () => {
    const { result, push } = setup();
    act(() => result.current.setOpen(true));
    const event = new KeyboardEvent("keydown", {
      key: "2", metaKey: true, bubbles: true, cancelable: true,
    });
    act(() => document.dispatchEvent(event));
    expect(push).toHaveBeenCalledExactlyOnceWith("/beta/issues");
    expect(result.current.isOpen).toBe(false);
    expect(event.defaultPrevented).toBe(true);
  });

  it("switches with arrows, wraps in menu order and stays open across workspace changes", () => {
    const { result, push, rerender } = setup("three");
    act(() => result.current.setOpen(true));
    fireEvent.keyDown(document, { key: "ArrowDown", metaKey: true });
    expect(push).toHaveBeenLastCalledWith("/alpha/issues");
    expect(result.current.isOpen).toBe(true);
    rerender({ currentId: "one" });
    fireEvent.keyDown(document, { key: "ArrowUp", metaKey: true });
    expect(push).toHaveBeenLastCalledWith("/gamma/issues");
    expect(result.current.isOpen).toBe(true);
  });

  it("leaves numbered and arrow shortcuts untouched while closed", () => {
    const { push } = setup();
    for (const key of ["2", "ArrowDown", "ArrowUp"]) {
      const event = new KeyboardEvent("keydown", { key, metaKey: true, bubbles: true, cancelable: true });
      act(() => document.dispatchEvent(event));
      expect(event.defaultPrevented).toBe(false);
    }
    expect(push).not.toHaveBeenCalled();
  });

  it("ignores missing positions and closes the current workspace without leaving its page", () => {
    const { result, push } = setup();
    act(() => result.current.setOpen(true));
    fireEvent.keyDown(document, { key: "9", metaKey: true });
    expect(result.current.isOpen).toBe(true);
    expect(push).not.toHaveBeenCalled();
    fireEvent.keyDown(document, { key: "1", metaKey: true });
    expect(result.current.isOpen).toBe(false);
    expect(push).not.toHaveBeenCalled();
  });

  it("executes a numbered selection delivered through desktop IPC", () => {
    const { result, push } = setup();
    act(() => result.current.setOpen(true));
    act(() => useWorkspaceSwitcherStore.getState().requestSelection(1));
    expect(push).toHaveBeenCalledExactlyOnceWith("/beta/issues");
    expect(result.current.isOpen).toBe(false);
  });

  it("uses Control on Windows and leaves extra modifiers and repeated digits alone", () => {
    configureShortcutPlatform("windows");
    const { result, push } = setup();
    act(() => result.current.setOpen(true));
    fireEvent.keyDown(document, { key: "2", metaKey: true });
    fireEvent.keyDown(document, { key: "2", ctrlKey: true, altKey: true });
    fireEvent.keyDown(document, { key: "2", ctrlKey: true, repeat: true });
    expect(push).not.toHaveBeenCalled();
    fireEvent.keyDown(document, { key: "2", ctrlKey: true });
    expect(push).toHaveBeenCalledExactlyOnceWith("/beta/issues");
  });
});
