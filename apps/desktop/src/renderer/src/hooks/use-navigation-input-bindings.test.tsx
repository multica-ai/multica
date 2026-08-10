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

  it.each([
    ["[", "Cmd+["],
    ["ArrowLeft", "Cmd+Left"],
  ])("goes back on %s", (key) => {
    render(<Probe />);
    const event = keydown({ key, metaKey: true });
    expect(activeHistoryIndex()).toBe(0);
    expect(event.defaultPrevented).toBe(true);
  });

  it.each([
    ["]", "Cmd+]"],
    ["ArrowRight", "Cmd+Right"],
  ])("goes forward on %s", (key) => {
    useTabStore.getState().goBack();
    render(<Probe />);
    keydown({ key, ctrlKey: true });
    expect(activeHistoryIndex()).toBe(1);
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
    keydown({ key: "[", metaKey: true, shiftKey: true });
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
    keydown({ key: "[", metaKey: true });
    expect(activeHistoryIndex()).toBe(1);
  });
});
