import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useModalStore } from "@multica/core/modals";
import { configureShortcutPlatform, createShortcutChord, useShortcutStore } from "@multica/core/shortcuts";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { GlobalShortcuts, SHORTCUT_SEQUENCE_TIMEOUT_MS } from "./global-shortcuts";

vi.mock("@multica/ui/components/ui/sidebar", () => ({
  useSidebar: () => ({ toggleSidebar: vi.fn() }),
}));
vi.mock("@multica/core/chat", () => ({
  useChatStore: { getState: () => ({ floatingChatEnabled: false }) },
}));
const viewer = vi.hoisted(() => ({ guest: false }));
vi.mock("@multica/core/permissions", () => ({ useIsGuest: () => viewer.guest }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "w" }),
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

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/w/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function press(key: string, init: KeyboardEventInit = {}, target: EventTarget = document.body) {
  const event = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...init });
  target.dispatchEvent(event);
  return event;
}

describe("GlobalShortcuts sequences", () => {
  const pageKey = vi.fn();

  const renderShortcuts = () =>
    render(
      <NavigationProvider value={adapter}>
        <GlobalShortcuts />
      </NavigationProvider>,
    );

  beforeEach(() => {
    configureShortcutPlatform("macos");
    vi.clearAllMocks();
    viewer.guest = false;
    // A page's own single-key shortcut, as the issue verbs (P, A) listen.
    document.addEventListener("keydown", pageKey);
    renderShortcuts();
  });

  afterEach(() => {
    document.removeEventListener("keydown", pageKey);
    configureShortcutPlatform(null);
    vi.useRealTimers();
    act(() => {
      useShortcutStore.getState().resetAll();
      useModalStore.getState().close();
    });
  });

  it.each([
    ["I", "/w/inbox"],
    ["M", "/w/my-issues"],
    ["P", "/w/projects"],
    ["C", "/w/chat"],
    ["A", "/w/agents"],
  ])("goes somewhere on G then %s", (key, path) => {
    expect(press("g").defaultPrevented).toBe(true);
    expect(press(key.toLowerCase()).defaultPrevented).toBe(true);
    expect(adapter.push).toHaveBeenCalledWith(path);
  });

  it("takes G then T on the Issues page without going anywhere", () => {
    press("g");
    expect(press("t").defaultPrevented).toBe(true);
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("keeps the second key from the page's own shortcuts", () => {
    press("g");
    press("p");
    expect(pageKey).toHaveBeenCalledTimes(0);

    press("p");
    expect(pageKey).toHaveBeenCalledTimes(1);
  });

  it("lets any other second key through with its own meaning", () => {
    press("g");
    const event = press("j");
    expect(event.defaultPrevented).toBe(false);
    expect(pageKey).toHaveBeenCalledTimes(1);
    expect(adapter.push).not.toHaveBeenCalled();

    press("i");
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("waits for the second key only so long", () => {
    vi.useFakeTimers();
    press("g");
    vi.advanceTimersByTime(SHORTCUT_SEQUENCE_TIMEOUT_MS + 1);
    press("i");
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("waits through Shift on the way to a shifted second key", () => {
    act(() =>
      useShortcutStore
        .getState()
        .setShortcut("goSettings", { ...createShortcutChord("G"), then: createShortcutChord("S", { shift: true }) }),
    );
    press("g");
    press("Shift", { shiftKey: true });
    press("S", { shiftKey: true });
    expect(adapter.push).toHaveBeenCalledWith("/w/settings");
  });

  it("lets a field, a dialog or a modal focused after G keep the second key", () => {
    const input = document.createElement("input");
    document.body.appendChild(input);
    press("g");
    expect(press("i", {}, input).defaultPrevented).toBe(false);
    expect(pageKey).toHaveBeenCalledTimes(1);
    // The sequence ended there: the next key on the page is not its second.
    press("p");
    input.remove();

    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    document.body.appendChild(dialog);
    press("g");
    expect(press("m", {}, dialog).defaultPrevented).toBe(false);
    dialog.remove();

    press("g");
    act(() => useModalStore.getState().open("create-issue"));
    expect(press("t").defaultPrevented).toBe(false);
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("does not start in a field, a dialog or a modal", () => {
    const input = document.createElement("input");
    document.body.appendChild(input);
    expect(press("g", {}, input).defaultPrevented).toBe(false);
    press("i", {}, input);
    input.remove();

    const dialog = document.createElement("div");
    dialog.setAttribute("role", "dialog");
    document.body.appendChild(dialog);
    press("g", {}, dialog);
    press("i", {}, dialog);
    dialog.remove();

    act(() => useModalStore.getState().open("create-issue"));
    press("g");
    press("i");
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("leaves every key to a shortcut being recorded", () => {
    const recorder = document.createElement("button");
    recorder.setAttribute("data-shortcut-recorder", "");
    document.body.appendChild(recorder);
    expect(press("g", {}, recorder).defaultPrevented).toBe(false);
    expect(press("u", {}, recorder).defaultPrevented).toBe(false);
    expect(pageKey).toHaveBeenCalledTimes(2);

    press("g");
    press("i", {}, recorder);
    recorder.remove();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("follows a rebound sequence, and the old one no longer", () => {
    act(() =>
      useShortcutStore.getState().setShortcut("goInbox", { ...createShortcutChord("G"), then: createShortcutChord("N") }),
    );
    press("g");
    press("i");
    expect(adapter.push).not.toHaveBeenCalled();
    press("g");
    press("n");
    expect(adapter.push).toHaveBeenCalledWith("/w/inbox");
  });

  it("leaves a guest's keys for pages it cannot open unclaimed", () => {
    cleanup();
    viewer.guest = true;
    renderShortcuts();
    press("g");
    expect(press("c").defaultPrevented).toBe(false);
    expect(adapter.push).not.toHaveBeenCalled();

    press("g");
    press("i");
    expect(adapter.push).toHaveBeenCalledWith("/w/inbox");
  });

  it("opens the shortcut sheet on ?", () => {
    const event = press("?", { shiftKey: true });
    expect(event.defaultPrevented).toBe(true);
    expect(useModalStore.getState().modal).toBe("keyboard-shortcuts");
  });
});
