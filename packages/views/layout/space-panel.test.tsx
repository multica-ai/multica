import { act, fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";
import { AppShellProvider, useAppShell } from "./app-shell-context";
import { SpacePanel } from "./space-panel";

const navigation = vi.hoisted(() => ({ pathname: "/acme/issues" }));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: navigation.pathname }),
}));
vi.mock("./panels/work-panel", () => ({
  WorkPanel: () => <div data-testid="work-panel" />,
  KeptRowsRelease: () => null,
}));
vi.mock("./panels/team-panel", () => ({ TeamPanel: () => <div data-testid="team-panel" /> }));
vi.mock("./panels/autopilots-panel", () => ({
  AutopilotsPanel: () => <div data-testid="autopilots-panel" />,
}));
vi.mock("./panels/list-space-peeks", () => ({
  InboxPeek: () => <div data-testid="inbox-peek" />,
  ChatPeek: () => <div data-testid="chat-peek" />,
}));

let shell: ReturnType<typeof useAppShell> = null;
let sidebar: ReturnType<typeof useSidebar> | null = null;
function Reader() {
  shell = useAppShell();
  sidebar = useSidebar();
  return null;
}

function renderPanel() {
  return renderWithI18n(
    <SidebarProvider>
      <AppShellProvider>
        <Reader />
        <SpacePanel />
      </AppShellProvider>
    </SidebarProvider>,
  );
}

const panel = () => document.querySelector('[data-slot="space-panel"]');
const peek = () => document.querySelector('[data-slot="space-panel-peek"]');

beforeEach(() => {
  navigation.pathname = "/acme/issues";
  localStorage.clear();
});

describe("SpacePanel", () => {
  it("renders the current space's panel", () => {
    renderPanel();
    expect(panel()).toHaveAttribute("data-state", "expanded");
    expect(screen.getByTestId("work-panel")).toBeInTheDocument();
  });

  // Inbox, Chat and Settings render their own list column in the card.
  it("renders nothing for spaces that bring their own column", () => {
    navigation.pathname = "/acme/inbox";
    renderPanel();
    expect(panel()).toBeNull();
  });

  it("collapses from a click on its edge and keeps its content out of reach", () => {
    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "Toggle left sidebar" }));
    expect(panel()).toHaveAttribute("data-state", "collapsed");
    expect(panel()).toHaveAttribute("inert");
  });

  it("commits a dragged width without toggling", () => {
    renderPanel();
    const handle = screen.getByRole("button", { name: "Toggle left sidebar" });
    vi.spyOn(panel()!, "getBoundingClientRect").mockReturnValue({ width: 240 } as DOMRect);

    fireEvent.pointerDown(handle, { button: 0, isPrimary: true, pointerId: 1, clientX: 240 });
    act(() => {
      document.dispatchEvent(new PointerEvent("pointermove", { pointerId: 1, clientX: 300 }));
    });
    expect((panel() as HTMLElement).style.getPropertyValue("--sidebar-width")).toBe("300px");
    act(() => {
      document.dispatchEvent(new PointerEvent("pointerup", { pointerId: 1, clientX: 300 }));
    });
    fireEvent.click(handle);

    expect(localStorage.getItem("sidebar_width")).toBe("300");
    expect(panel()).toHaveAttribute("data-state", "expanded");
  });

  it("previews a hovered space over the page while collapsed", () => {
    vi.useFakeTimers();
    try {
      renderPanel();
      act(() => sidebar!.setOpen(false));
      act(() => shell!.requestPeek("team"));
      act(() => vi.advanceTimersByTime(200));
      expect(peek()).not.toBeNull();
      expect(screen.getByTestId("team-panel")).toBeInTheDocument();

      act(() => sidebar!.setOpen(true));
      expect(peek()).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("previews Inbox and Chat from another space, not over their own page", () => {
    vi.useFakeTimers();
    try {
      renderPanel();
      act(() => shell!.toggleNav());
      act(() => shell!.requestPeek("inbox"));
      act(() => vi.advanceTimersByTime(200));
      expect(screen.getByTestId("inbox-peek")).toBeInTheDocument();
      act(() => shell!.requestPeek("chat"));
      expect(screen.getByTestId("chat-peek")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  // There the page's own list column floats instead (ListColumn).
  it("leaves the current list space's preview to its list column", () => {
    navigation.pathname = "/acme/inbox";
    renderPanel();
    act(() => shell!.toggleNav());
    act(() => shell!.requestPeek("inbox"));
    act(() => shell!.requestPeek("inbox"));
    expect(peek()).toBeNull();
  });
});
