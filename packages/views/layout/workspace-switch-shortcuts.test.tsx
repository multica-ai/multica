import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import {
  WorkspaceSwitchShortcuts,
  workspaceSwitchIndex,
  workspaceSwitchShortcut,
} from "./workspace-switch-shortcuts";

const { mockWorkspaces, mockCurrent } = vi.hoisted(() => ({
  mockWorkspaces: {
    current: [
      { id: "ws-a", slug: "media-lab", name: "Media Lab" },
      { id: "ws-b", slug: "studio", name: "Studio" },
      { id: "ws-c", slug: "repos", name: "Repos" },
    ],
  },
  mockCurrent: { current: { id: "ws-a" } as { id: string } | null },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mockWorkspaces.current }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces", "list"] }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => mockCurrent.current,
}));

function key(code: string, init: KeyboardEventInit = {}) {
  return new KeyboardEvent("keydown", { code, bubbles: true, cancelable: true, ...init });
}

function renderShortcuts() {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/media-lab/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  const view = render(
    <NavigationProvider value={adapter}>
      <WorkspaceSwitchShortcuts />
    </NavigationProvider>,
  );
  return { adapter, ...view };
}

describe("workspaceSwitchIndex", () => {
  it("reads the physical digit under Ctrl+Alt and Alt+Shift", () => {
    expect(workspaceSwitchIndex(key("Digit1", { ctrlKey: true, altKey: true }))).toBe(0);
    expect(workspaceSwitchIndex(key("Digit9", { altKey: true, shiftKey: true }))).toBe(8);
  });

  it("accepts an Option key reported as AltGraph (Mac keyboard on Linux)", () => {
    const altGr = (code: string, init: KeyboardEventInit) => {
      const event = key(code, init);
      Object.defineProperty(event, "getModifierState", { value: (k: string) => k === "AltGraph" });
      return event;
    };
    expect(workspaceSwitchIndex(altGr("Digit2", { shiftKey: true }))).toBe(1);
    expect(workspaceSwitchIndex(altGr("Digit2", { ctrlKey: true }))).toBe(1);
  });

  it("accepts Ctrl+Meta, the Option key of a Mac keyboard in Windows mode", () => {
    expect(workspaceSwitchIndex(key("Digit2", { ctrlKey: true, metaKey: true }))).toBe(1);
  });

  it("ignores the logical key, so AZERTY and shifted symbols still map", () => {
    // AZERTY top row: the "3" key reports `"` unshifted, `3` with Shift.
    expect(workspaceSwitchIndex(key("Digit3", { key: "3", altKey: true, shiftKey: true }))).toBe(2);
    expect(workspaceSwitchIndex(key("Digit3", { key: "#", ctrlKey: true, altKey: true }))).toBe(2);
  });

  it("rejects every other chord", () => {
    expect(workspaceSwitchIndex(key("Digit1", { altKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("Digit1", { shiftKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("Digit1", { ctrlKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("Digit1", { ctrlKey: true, altKey: true, metaKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("Digit1", { metaKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("Digit1", { metaKey: true, shiftKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("Digit0", { ctrlKey: true, altKey: true }))).toBeNull();
    expect(workspaceSwitchIndex(key("KeyA", { ctrlKey: true, altKey: true }))).toBeNull();
  });
});

describe("workspaceSwitchShortcut", () => {
  it("labels the first nine positions only", () => {
    expect(workspaceSwitchShortcut(0)?.key).toBe("1");
    expect(workspaceSwitchShortcut(0)?.modifiers).toMatchObject({ alt: true, shift: false, meta: false });
    expect(workspaceSwitchShortcut(8)?.key).toBe("9");
    expect(workspaceSwitchShortcut(9)).toBeNull();
  });
});

describe("WorkspaceSwitchShortcuts", () => {
  it("opens the Nth workspace of the list", () => {
    const { adapter, unmount } = renderShortcuts();
    const event = key("Digit2", { ctrlKey: true, altKey: true });
    document.dispatchEvent(event);
    expect(adapter.push).toHaveBeenCalledWith("/studio/issues");
    expect(event.defaultPrevented).toBe(true);
    unmount();
  });

  it("does nothing for the current workspace or a position past the list", () => {
    const { adapter, unmount } = renderShortcuts();
    document.dispatchEvent(key("Digit1", { altKey: true, shiftKey: true }));
    document.dispatchEvent(key("Digit7", { altKey: true, shiftKey: true }));
    expect(adapter.push).not.toHaveBeenCalled();
    unmount();
  });

  it("leaves the chord to a focused text field", () => {
    const { adapter, unmount } = renderShortcuts();
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.dispatchEvent(key("Digit2", { altKey: true, shiftKey: true }));
    expect(adapter.push).not.toHaveBeenCalled();
    input.remove();
    unmount();
  });
});
