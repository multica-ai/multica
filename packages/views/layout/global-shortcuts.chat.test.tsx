import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { configureShortcutPlatform } from "@multica/core/shortcuts";
import { GlobalShortcuts } from "./global-shortcuts";

// The floating chat overlay is reachable from the keyboard (MUL-5522). What
// matters is not just "the chord calls toggle", but that it stays a no-op —
// without swallowing the keypress — everywhere the overlay cannot exist.
const h = vi.hoisted(() => ({
  chat: { floatingChatEnabled: true, toggle: vi.fn() },
  navigation: { pathname: "/acme/issues", push: vi.fn() },
  toggleSidebar: vi.fn(),
  searchToggle: vi.fn(),
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof h.chat) => unknown) => selector(h.chat),
    { getState: () => h.chat },
  ),
}));
vi.mock("@multica/core/issues/stores", () => ({
  openCreateIssueWithPreference: vi.fn(),
}));
vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ modal: null }) },
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
    chat: () => "/acme/chat",
    myIssues: () => "/acme/my-issues",
    issues: () => "/acme/issues",
    projects: () => "/acme/projects",
    autopilots: () => "/acme/autopilots",
    agents: () => "/acme/agents",
    squads: () => "/acme/squads",
    usage: () => "/acme/usage",
    runtimes: () => "/acme/runtimes",
    skills: () => "/acme/skills",
    settings: () => "/acme/settings",
  }),
}));
vi.mock("@multica/ui/components/ui/sidebar", () => ({
  useSidebar: () => ({ toggleSidebar: h.toggleSidebar }),
}));
vi.mock("../navigation", () => ({ useNavigation: () => h.navigation }));
vi.mock("../search/search-store", () => ({
  useSearchStore: { getState: () => ({ toggle: h.searchToggle }) },
}));

/** Mod+J on macOS, dispatched the way a real keypress reaches the document. */
function pressToggleChat(target: EventTarget = document): boolean {
  const event = new KeyboardEvent("keydown", {
    key: "j",
    metaKey: true,
    bubbles: true,
    cancelable: true,
  });
  target.dispatchEvent(event);
  return event.defaultPrevented;
}

beforeEach(() => {
  // Pin the platform: jsdom's user agent reports the host OS, so Command vs
  // Control would otherwise depend on where the suite runs.
  configureShortcutPlatform("macos");
  h.chat.floatingChatEnabled = true;
  h.navigation.pathname = "/acme/issues";
});

afterEach(() => {
  configureShortcutPlatform(null);
  vi.clearAllMocks();
});

describe("chat toggle shortcut", () => {
  it("toggles the floating window and consumes the chord", () => {
    render(<GlobalShortcuts />);

    expect(pressToggleChat()).toBe(true);
    expect(h.chat.toggle).toHaveBeenCalledTimes(1);
  });

  it("still fires while the caret is inside a text input", () => {
    render(<GlobalShortcuts />);
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    expect(pressToggleChat(input)).toBe(true);
    expect(h.chat.toggle).toHaveBeenCalledTimes(1);

    input.remove();
  });

  it("leaves the chord alone on the Chat tab, where the overlay is suppressed", () => {
    h.navigation.pathname = "/acme/chat";
    render(<GlobalShortcuts />);

    // Not prevented: an action that cannot run must not swallow the keypress.
    expect(pressToggleChat()).toBe(false);
    expect(h.chat.toggle).not.toHaveBeenCalled();
  });

  it("leaves the chord alone when the floating window is turned off", () => {
    h.chat.floatingChatEnabled = false;
    render(<GlobalShortcuts />);

    expect(pressToggleChat()).toBe(false);
    expect(h.chat.toggle).not.toHaveBeenCalled();
  });

  it("reads the preference at press time, not at mount", () => {
    render(<GlobalShortcuts />);

    h.chat.floatingChatEnabled = false;
    expect(pressToggleChat()).toBe(false);

    h.chat.floatingChatEnabled = true;
    expect(pressToggleChat()).toBe(true);
    expect(h.chat.toggle).toHaveBeenCalledTimes(1);
  });

  it("does not confuse the chat chord with the other global bindings", () => {
    render(<GlobalShortcuts />);

    document.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "b",
        metaKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );
    expect(h.toggleSidebar).toHaveBeenCalledTimes(1);
    expect(h.chat.toggle).not.toHaveBeenCalled();
  });
});
