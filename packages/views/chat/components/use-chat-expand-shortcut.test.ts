import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import {
  configureShortcutPlatform,
  createShortcutChord,
  useShortcutStore,
} from "@multica/core/shortcuts";
import { useChatExpandShortcut } from "./use-chat-expand-shortcut";

const h = vi.hoisted(() => ({
  chat: {
    isOpen: true,
    setOpen: vi.fn(),
    setExpanded: vi.fn(),
  },
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof h.chat) => unknown) => selector(h.chat),
    { getState: () => h.chat },
  ),
}));

/** Alt+Mod+E on macOS, a chord the user bound in Settings → Shortcuts. */
const BOUND = createShortcutChord("E", { primary: true, alt: true });

function press(init: KeyboardEventInit = {}): boolean {
  const event = new KeyboardEvent("keydown", {
    key: "e",
    code: "KeyE",
    metaKey: true,
    altKey: true,
    bubbles: true,
    cancelable: true,
    ...init,
  });
  document.dispatchEvent(event);
  return event.defaultPrevented;
}

describe("useChatExpandShortcut", () => {
  beforeEach(() => {
    configureShortcutPlatform("macos");
    useShortcutStore.getState().setShortcut("toggleChatExpand", BOUND);
    h.chat.isOpen = true;
    h.chat.setOpen.mockClear();
    h.chat.setExpanded.mockClear();
  });

  afterEach(() => {
    useShortcutStore.getState().resetShortcut("toggleChatExpand");
    configureShortcutPlatform(null);
  });

  it("runs the window's own expand toggle while the window is open", () => {
    const toggleExpand = vi.fn();
    renderHook(() => useChatExpandShortcut(true, toggleExpand));

    expect(press()).toBe(true);
    expect(toggleExpand).toHaveBeenCalledTimes(1);
    expect(h.chat.setOpen).not.toHaveBeenCalled();
  });

  it("opens a closed window already expanded", () => {
    h.chat.isOpen = false;
    const toggleExpand = vi.fn();
    renderHook(() => useChatExpandShortcut(true, toggleExpand));

    expect(press()).toBe(true);
    expect(h.chat.setExpanded).toHaveBeenCalledWith(true);
    expect(h.chat.setOpen).toHaveBeenCalledWith(true);
    expect(toggleExpand).not.toHaveBeenCalled();
  });

  it("leaves the keypress alone where the window has no expand button", () => {
    const toggleExpand = vi.fn();
    renderHook(() => useChatExpandShortcut(false, toggleExpand));

    expect(press()).toBe(false);
    expect(toggleExpand).not.toHaveBeenCalled();
  });

  it("does nothing until the user binds a key", () => {
    useShortcutStore.getState().resetShortcut("toggleChatExpand");
    const toggleExpand = vi.fn();
    renderHook(() => useChatExpandShortcut(true, toggleExpand));

    expect(press()).toBe(false);
    expect(toggleExpand).not.toHaveBeenCalled();
  });

  it("ignores held-down repeats and other modifiers", () => {
    const toggleExpand = vi.fn();
    renderHook(() => useChatExpandShortcut(true, toggleExpand));

    press({ repeat: true });
    press({ altKey: false });
    expect(toggleExpand).not.toHaveBeenCalled();
  });

  it("stops listening when the window unmounts", () => {
    const toggleExpand = vi.fn();
    const { unmount } = renderHook(() => useChatExpandShortcut(true, toggleExpand));
    unmount();

    expect(press()).toBe(false);
    expect(toggleExpand).not.toHaveBeenCalled();
  });
});
