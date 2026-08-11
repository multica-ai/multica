// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { useTabStore } from "@/stores/tab-store";
import { useNavigationInputBindings } from "./use-tab-history";

function Probe() {
  useNavigationInputBindings();
  return null;
}

/** Active tab sitting on entry index 1 of a two-entry history. */
function seedHistory() {
  useTabStore.setState({
    activeWorkspaceSlug: "acme",
    byWorkspace: {
      acme: {
        tabs: [
          {
            id: "t1",
            url: "/acme/issues/abc",
            resourceKey: "/acme/issues/abc",
            title: "",
            pinned: false,
            history: {
              stack: ["/acme/issues", "/acme/issues/abc"],
              index: 1,
            },
            memento: { scroll: {}, view: {} },
          },
        ],
        activeTabId: "t1",
        recentTabIds: ["t1"],
      },
    },
  });
}

function activeHistoryIndex(): number {
  return useTabStore.getState().byWorkspace.acme.tabs[0].history.index;
}

function keydown(init: KeyboardEventInit, target: EventTarget = window) {
  const event = new KeyboardEvent("keydown", {
    bubbles: true,
    cancelable: true,
    ...init,
  });
  target.dispatchEvent(event);
  return event;
}

describe("useNavigationInputBindings", () => {
  beforeEach(() => {
    seedHistory();
  });

  it("goes back on Cmd+Left", () => {
    render(<Probe />);
    const event = keydown({ key: "ArrowLeft", metaKey: true });
    expect(activeHistoryIndex()).toBe(0);
    expect(event.defaultPrevented).toBe(true);
  });

  it("goes forward on Cmd+Right", () => {
    useTabStore.getState().goBack();
    render(<Probe />);
    keydown({ key: "ArrowRight", ctrlKey: true });
    expect(activeHistoryIndex()).toBe(1);
  });

  // #6728: the Cmd/Ctrl+[ and +] chords moved to the shared shortcut registry
  // (packages/core/shortcuts, driven by <GlobalShortcuts>). This hook must NOT
  // also handle them — DesktopShell mounts both this window listener and the
  // document-level GlobalShortcuts, so a bracket handled here too would
  // double-navigate (one press = two steps, hidden below a history depth of 3
  // by stepHistory's clamp). It must also not preventDefault, so the registry
  // still receives the keydown.
  it.each([
    ["[", "Cmd+["],
    ["]", "Cmd+]"],
  ])("leaves %s to the shortcut registry (no double navigation)", (key) => {
    render(<Probe />);
    const before = activeHistoryIndex();
    const event = keydown({ key, metaKey: true });
    expect(activeHistoryIndex()).toBe(before);
    expect(event.defaultPrevented).toBe(false);
  });

  it("leaves the chord to editable targets (caret navigation wins)", () => {
    render(<Probe />);
    const input = document.createElement("input");
    document.body.appendChild(input);
    const event = keydown({ key: "ArrowLeft", metaKey: true }, input);
    expect(activeHistoryIndex()).toBe(1);
    expect(event.defaultPrevented).toBe(false);
    input.remove();
  });

  it("ignores the chord with extra modifiers held", () => {
    render(<Probe />);
    keydown({ key: "ArrowLeft", metaKey: true, shiftKey: true });
    expect(activeHistoryIndex()).toBe(1);
  });

  it("navigates on mouse side buttons 3 (back) and 4 (forward)", () => {
    render(<Probe />);
    window.dispatchEvent(new MouseEvent("mouseup", { button: 3 }));
    expect(activeHistoryIndex()).toBe(0);
    window.dispatchEvent(new MouseEvent("mouseup", { button: 4 }));
    expect(activeHistoryIndex()).toBe(1);
  });

  it("stops listening after unmount", () => {
    const view = render(<Probe />);
    view.unmount();
    keydown({ key: "ArrowLeft", metaKey: true });
    expect(activeHistoryIndex()).toBe(1);
  });
});
