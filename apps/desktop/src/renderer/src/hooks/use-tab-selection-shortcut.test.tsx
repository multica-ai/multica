// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import type { TabSelectionShortcutKey } from "../../../shared/main-renderer-messages";
import { useTabStore, type TabSession } from "@/stores/tab-store";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";
import {
  selectTabByShortcut,
  useTabSelectionShortcut,
} from "./use-tab-selection-shortcut";

function Probe() {
  useTabSelectionShortcut();
  return null;
}

function makeTab(index: number): TabSession {
  const url = `/acme/issues/${index}`;
  return {
    id: `t${index}`,
    url,
    resourceKey: url,
    title: `Tab ${index}`,
    pinned: index === 1,
    history: { stack: [url, `${url}?view=detail`], index: 1 },
    memento: {
      scroll: { [`${url}::main`]: { top: index * 10, height: 1000 } },
      view: { [`${url}::selection`]: `item-${index}` },
    },
  };
}

function seedTabs(count: number, activeIndex = 1): void {
  const tabs = Array.from({ length: count }, (_, index) => makeTab(index + 1));
  useTabStore.setState({
    activeWorkspaceSlug: "acme",
    byWorkspace: {
      acme: {
        tabs,
        activeTabId: `t${activeIndex}`,
        recentTabIds: [],
      },
    },
  });
}

function activeTabId(): string | undefined {
  return useTabStore.getState().byWorkspace.acme?.activeTabId;
}

function stubDesktopAPI(kind: "main" | "issue") {
  let handler: ((key: TabSelectionShortcutKey) => void) | null = null;
  const unsubscribe = vi.fn();
  const onSelectTabShortcut = vi.fn(
    (callback: (key: TabSelectionShortcutKey) => void) => {
      handler = callback;
      return unsubscribe;
    },
  );
  (window as unknown as { desktopAPI: Record<string, unknown> }).desktopAPI = {
    windowContext:
      kind === "main"
        ? { kind: "main" }
        : { kind: "issue", path: "/acme/issues/abc", workspaceSlug: "acme" },
    onSelectTabShortcut,
  };
  return {
    onSelectTabShortcut,
    unsubscribe,
    deliver: (key: TabSelectionShortcutKey) => handler?.(key),
  };
}

describe("selectTabByShortcut", () => {
  beforeEach(() => {
    seedTabs(10);
    useWindowOverlayStore.setState({ overlay: null });
  });

  it.each([1, 2, 3, 4, 5, 6, 7, 8] as const)(
    "selects exact tab position %i",
    (key) => {
      selectTabByShortcut(key);
      expect(activeTabId()).toBe(`t${key}`);
    },
  );

  it.each([1, 3, 10])(
    "selects the last of %i tabs for shortcut 9",
    (tabCount) => {
      seedTabs(tabCount);
      selectTabByShortcut(9);
      expect(activeTabId()).toBe(`t${tabCount}`);
    },
  );

  it("does nothing when the requested exact position does not exist", () => {
    seedTabs(3, 2);
    selectTabByShortcut(8);
    expect(activeTabId()).toBe("t2");
  });

  it("preserves every tab session while changing selection", () => {
    const before = useTabStore.getState().byWorkspace.acme.tabs;

    selectTabByShortcut(7);

    const group = useTabStore.getState().byWorkspace.acme;
    expect(group.activeTabId).toBe("t7");
    expect(group.tabs).toBe(before);
    expect(group.tabs).toEqual(before);
  });

  it("does nothing without a visible tab group", () => {
    useTabStore.setState({ activeWorkspaceSlug: null });
    expect(() => selectTabByShortcut(1)).not.toThrow();

    useTabStore.setState({ activeWorkspaceSlug: "missing" });
    expect(() => selectTabByShortcut(9)).not.toThrow();
  });

  it("does not switch a hidden tab while a window overlay is active", () => {
    useWindowOverlayStore.setState({ overlay: { type: "onboarding" } });
    selectTabByShortcut(5);
    expect(activeTabId()).toBe("t1");
  });
});

describe("useTabSelectionShortcut", () => {
  beforeEach(() => {
    seedTabs(10);
    useWindowOverlayStore.setState({ overlay: null });
  });

  it("selects a tab when main delivers the fixed shortcut", () => {
    const { deliver } = stubDesktopAPI("main");
    render(<Probe />);

    deliver(4);

    expect(activeTabId()).toBe("t4");
  });

  it("unsubscribes when the main renderer unmounts", () => {
    const { unsubscribe } = stubDesktopAPI("main");
    const view = render(<Probe />);

    view.unmount();

    expect(unsubscribe).toHaveBeenCalledOnce();
  });

  it("does not subscribe in a dedicated issue window", () => {
    const { onSelectTabShortcut } = stubDesktopAPI("issue");

    render(<Probe />);

    expect(onSelectTabShortcut).not.toHaveBeenCalled();
  });
});
