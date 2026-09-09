// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { configureShortcutPlatform } from "@multica/core/shortcuts";
import { useTabStore } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";
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

function seedTabs() {
  seedHistory();
  useTabStore.setState((state) => ({
    byWorkspace: {
      acme: {
        ...state.byWorkspace.acme,
        tabs: [
          state.byWorkspace.acme.tabs[0],
          {
            ...state.byWorkspace.acme.tabs[0],
            id: "t2",
            url: "/acme/projects",
            resourceKey: "/acme/projects",
          },
          {
            ...state.byWorkspace.acme.tabs[0],
            id: "t3",
            url: "/acme/skills",
            resourceKey: "/acme/skills",
          },
        ],
      },
    },
  }));
}

function activeTabId(): string {
  return useTabStore.getState().byWorkspace.acme.activeTabId;
}

function stubWindowContext(kind: "main" | "issue") {
  (window as unknown as { desktopAPI: Record<string, unknown> }).desktopAPI = {
    windowContext:
      kind === "main"
        ? { kind: "main" }
        : { kind: "issue", path: "/acme/issues/abc", workspaceSlug: "acme" },
  };
}

function activeHistoryIndex(): number {
  return useTabStore.getState().byWorkspace.acme.tabs[0].history.index;
}

function keydown(
  init: KeyboardEventInit & { keyCode?: number },
  target: EventTarget = window,
) {
  const event = new KeyboardEvent("keydown", {
    bubbles: true,
    cancelable: true,
    ...init,
  });
  if (init.keyCode !== undefined) {
    Object.defineProperty(event, "keyCode", { value: init.keyCode });
  }
  target.dispatchEvent(event);
  return event;
}

describe("useNavigationInputBindings", () => {
  beforeEach(() => {
    seedHistory();
    configureShortcutPlatform("macos");
    useWindowOverlayStore.setState({ overlay: null });
    stubWindowContext("main");
  });

  afterEach(() => {
    configureShortcutPlatform(null);
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

  it.each(["macos", "windows", "linux"] as const)(
    "cycles tabs with Control+Tab on %s",
    (platform) => {
      seedTabs();
      configureShortcutPlatform(platform);
      render(<Probe />);

      const event = keydown({ key: "Tab", ctrlKey: true });

      expect(activeTabId()).toBe("t2");
      expect(event.defaultPrevented).toBe(true);
    },
  );

  it.each(["macos", "windows", "linux"] as const)(
    "cycles to the previous tab with Control+Shift+Tab on %s",
    (platform) => {
      seedTabs();
      configureShortcutPlatform(platform);
      render(<Probe />);

      keydown({ key: "Tab", ctrlKey: true, shiftKey: true });

      expect(activeTabId()).toBe("t3");
    },
  );

  it.each([
    ["[", "t3"],
    ["{", "t3"],
    ["]", "t2"],
    ["}", "t2"],
  ])("accepts the macOS shifted bracket alias %s", (key, expectedId) => {
    seedTabs();
    render(<Probe />);

    const event = keydown({ key, metaKey: true, shiftKey: true });

    expect(activeTabId()).toBe(expectedId);
    expect(event.defaultPrevented).toBe(true);
  });

  it.each(["windows", "linux"] as const)(
    "does not handle bracket aliases on %s",
    (platform) => {
      seedTabs();
      configureShortcutPlatform(platform);
      render(<Probe />);

      const event = keydown({ key: "]", ctrlKey: true, shiftKey: true });

      expect(activeTabId()).toBe("t1");
      expect(event.defaultPrevented).toBe(false);
    },
  );

  it.each([
    [{ key: "Tab", ctrlKey: true, repeat: true }, "repeat"],
    [{ key: "Tab", ctrlKey: true, isComposing: true }, "IME composition"],
    [{ key: "Tab", ctrlKey: true, keyCode: 229 }, "IME keyCode 229"],
    [{ key: "Tab", metaKey: true }, "missing Control"],
    [{ key: "Tab", ctrlKey: true, metaKey: true }, "extra Meta"],
    [{ key: "Tab", ctrlKey: true, altKey: true }, "extra Alt"],
  ])("ignores tab cycling with %s", (init, _reason) => {
    seedTabs();
    render(<Probe />);

    const event = keydown(init);

    expect(activeTabId()).toBe("t1");
    expect(event.defaultPrevented).toBe(false);
  });

  it("respects an earlier handler that prevents tab cycling", () => {
    seedTabs();
    render(<Probe />);
    const event = new KeyboardEvent("keydown", {
      key: "Tab",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    event.preventDefault();

    window.dispatchEvent(event);

    expect(activeTabId()).toBe("t1");
  });

  it("ignores tab cycling in editable targets and editable ancestors", () => {
    seedTabs();
    render(<Probe />);
    const editor = document.createElement("div");
    editor.setAttribute("contenteditable", "true");
    const target = document.createElement("span");
    editor.appendChild(target);
    document.body.appendChild(editor);

    const event = keydown({ key: "Tab", ctrlKey: true }, target);

    expect(activeTabId()).toBe("t1");
    expect(event.defaultPrevented).toBe(false);
    editor.remove();
  });

  it("ignores tab cycling while a full-window overlay is active", () => {
    seedTabs();
    useWindowOverlayStore.setState({ overlay: { type: "onboarding" } });
    render(<Probe />);

    keydown({ key: "Tab", ctrlKey: true });

    expect(activeTabId()).toBe("t1");
  });

  it("does not subscribe in a dedicated issue window", () => {
    seedTabs();
    stubWindowContext("issue");
    render(<Probe />);

    const event = keydown({ key: "Tab", ctrlKey: true });

    expect(activeTabId()).toBe("t1");
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
