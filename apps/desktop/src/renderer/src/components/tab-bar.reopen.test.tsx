import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { configureShortcutPlatform } from "@multica/core/shortcuts";
import { RESOURCES } from "@multica/views/locales";
import { getActiveTab, useTabStore } from "@/stores/tab-store";
import { useNavigationInputBindings } from "@/hooks/use-tab-history";
import { DesktopNavigationProvider } from "@/platform/navigation";
import { TabBar } from "./tab-bar";

// Presentation data is a query-cache concern. Keep the real strip, menu,
// shortcut hook and store, with deterministic display titles and no API.
vi.mock("@multica/views/layout", () => ({
  useTabPresentation: (_url: string, title: string) => ({ visual: {}, title }),
  ResourceLeadingVisual: () => <span />,
}));

function Shell() {
  useNavigationInputBindings();
  return (
    <I18nProvider locale="en" resources={RESOURCES}>
      <DesktopNavigationProvider><TabBar /></DesktopNavigationProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  });
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
  configureShortcutPlatform("macos");
  vi.stubGlobal("desktopAPI", {
    runtimeConfig: { ok: true, config: { appUrl: "https://fixture.invalid" } },
    windowContext: { kind: "main" },
    openIssueWindow: vi.fn(),
  });
});
afterEach(() => { cleanup(); configureShortcutPlatform(null); vi.unstubAllGlobals(); });

function seed() {
  const store = useTabStore.getState();
  const id = store.openTab("/acme/issues/ABC?filter=mine#comment-42", "Example issue", { activate: true });
  store.addTab("/acme/projects", "Projects");
  return id;
}

async function menuOn(name: string) {
  fireEvent.contextMenu(screen.getByRole("button", { name }), { button: 2 });
  return screen.findByRole("menu");
}

describe("closed destinations through the real tab strip", () => {
  it.each(["X", "middle", "menu"])("reopens after %s close with URL, position and a fresh session", async (entry) => {
    const id = seed();
    const original = getActiveTab(useTabStore.getState())!;
    useTabStore.getState().commitViewState(id, "/acme/issues/ABC", "draft", "old");
    render(<Shell />);
    const tab = screen.getByRole("button", { name: "Example issue" });
    if (entry === "X") fireEvent.click(within(tab).getByRole("button", { name: "Close tab" }));
    if (entry === "middle") fireEvent(tab, new MouseEvent("auxclick", { bubbles: true, button: 1 }));
    if (entry === "menu") {
      const menu = await menuOn("Example issue");
      fireEvent.click(within(menu).getByRole("menuitem", { name: "Close tab" }));
    }
    expect(useTabStore.getState().byWorkspace.acme.tabs.some((t) => t.id === id)).toBe(false);
    const menu = await menuOn("Issues");
    const reopen = within(menu).getByRole("menuitem", { name: /Reopen closed tab/i });
    expect(reopen).not.toHaveAttribute("aria-disabled", "true");
    fireEvent.click(reopen);
    const restored = getActiveTab(useTabStore.getState())!;
    expect(restored.url).toBe(original.url);
    expect(restored.id).not.toBe(id);
    expect(restored.history).toEqual({ stack: [original.url], index: 0 });
    expect(restored.memento).toEqual({ scroll: {}, view: {} });
    expect(useTabStore.getState().byWorkspace.acme.tabs[1].id).toBe(restored.id);
  });

  it("the mounted navigation hook recovers a real user close on Cmd+Shift+T", () => {
    const id = seed();
    const url = getActiveTab(useTabStore.getState())!.url;
    render(<Shell />);
    fireEvent.click(within(screen.getByRole("button", { name: "Example issue" })).getByRole("button", { name: "Close tab" }));
    expect(useTabStore.getState().byWorkspace.acme.tabs.some((t) => t.id === id)).toBe(false);
    fireEvent.keyDown(window, { key: "T", metaKey: true, shiftKey: true });
    expect(getActiveTab(useTabStore.getState())?.url).toBe(url);
  });

  it("disables reopening on an empty stack", async () => {
    render(<Shell />);
    const menu = await menuOn("Issues");
    expect(within(menu).getByRole("menuitem", { name: /Reopen closed tab/i })).toHaveAttribute("aria-disabled", "true");
  });

  it("Close Others preserves pinned survivors and restores removed tabs left to right", async () => {
    const store = useTabStore.getState();
    const pinned = store.byWorkspace.acme.tabs[0];
    store.togglePin(pinned.id);
    store.addTab("/acme/projects", "Projects");
    store.addTab("/acme/agents", "Agents");
    const keep = store.addTab("/acme/settings", "Settings");
    store.setActiveTab(keep);
    render(<Shell />);
    fireEvent.click(within(await menuOn("Settings")).getByRole("menuitem", { name: "Close other tabs" }));
    expect(useTabStore.getState().byWorkspace.acme.tabs.map((t) => t.title)).toEqual(["Issues", "Settings"]);
    fireEvent.click(within(await menuOn("Settings")).getByRole("menuitem", { name: /Reopen closed tab/i }));
    expect(getActiveTab(useTabStore.getState())?.title).toBe("Projects");
    fireEvent.keyDown(window, { key: "T", metaKey: true, shiftKey: true });
    expect(useTabStore.getState().byWorkspace.acme.tabs.map((t) => t.title)).toEqual(["Issues", "Projects", "Agents", "Settings"]);
    expect(useTabStore.getState().byWorkspace.acme.tabs[0]).toMatchObject({ id: pinned.id, pinned: true });
  });

  it("Close Right keeps pinned tabs and restores destinations left to right through the menu", async () => {
    const store = useTabStore.getState();
    const issues = store.byWorkspace.acme.tabs[0];
    store.togglePin(issues.id);
    const projects = store.addTab("/acme/projects", "Projects");
    store.togglePin(projects);
    store.addTab("/acme/agents", "Agents");
    const settings = store.addTab("/acme/settings", "Settings");
    store.setActiveTab(projects);
    store.setActiveTab(settings);
    render(<Shell />);

    fireEvent.click(within(await menuOn("Issues (pinned)")).getByRole("menuitem", { name: "Close tabs to the right" }));
    let group = useTabStore.getState().byWorkspace.acme;
    expect(group.tabs.map((tab) => tab.id)).toEqual([issues.id, projects]);
    expect(group.activeTabId).toBe(issues.id);
    expect(group.recentTabIds).toEqual([projects]);

    fireEvent.click(within(await menuOn("Issues (pinned)")).getByRole("menuitem", { name: /Reopen closed tab/i }));
    expect(getActiveTab(useTabStore.getState())?.title).toBe("Agents");
    fireEvent.click(within(await menuOn("Agents")).getByRole("menuitem", { name: /Reopen closed tab/i }));
    group = useTabStore.getState().byWorkspace.acme;
    expect(group.tabs.map((tab) => tab.title)).toEqual(["Issues", "Projects", "Agents", "Settings"]);
    expect(group.tabs.map((tab) => tab.pinned)).toEqual([true, true, false, false]);
    expect(group.recentTabIds).toEqual([group.tabs[2].id, issues.id, projects]);
    expect(within(await menuOn("Settings")).getByRole("menuitem", { name: /Reopen closed tab/i })).toHaveAttribute("aria-disabled", "true");
  });

  it("keeps the current menu order, shareable Copy link, pin and issue-window actions", async () => {
    seed();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", Object.create(navigator, {
      clipboard: { value: { writeText } },
    }));
    render(<Shell />);
    let menu = await menuOn("Example issue");
    const labels = [
      "Open as new window", "Pin tab", "Copy link", "Close tab", "Close other tabs",
      "Close tabs to the right", "Reopen closed tab",
    ];
    const items = within(menu).getAllByRole("menuitem");
    expect(items).toHaveLength(labels.length);
    items.forEach((item, index) => expect(item).toHaveAccessibleName(labels[index]));
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Copy link" }));
    expect(writeText).toHaveBeenCalledExactlyOnceWith("https://fixture.invalid/acme/issues/ABC?filter=mine#comment-42");
    menu = await menuOn("Example issue");
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Open as new window" }));
    expect(window.desktopAPI.openIssueWindow).toHaveBeenCalledExactlyOnceWith({
      path: "/acme/issues/ABC?filter=mine#comment-42", title: "Example issue",
    });
    fireEvent.click(within(await menuOn("Example issue")).getByRole("menuitem", { name: "Pin tab" }));
    expect(useTabStore.getState().byWorkspace.acme.tabs.find((tab) => tab.title === "Example issue")?.pinned).toBe(true);
  });
});
