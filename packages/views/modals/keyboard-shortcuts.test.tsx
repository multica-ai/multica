import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureShortcutPlatform, createShortcutChord, useShortcutStore } from "@multica/core/shortcuts";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { KeyboardShortcutsModal } from "./keyboard-shortcuts";

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ({ id: "w" }),
  useWorkspacePaths: () => ({
    inbox: () => "/w/inbox",
    issues: () => "/w/issues",
    myIssues: () => "/w/my-issues",
    views: () => "/w/views",
    projects: () => "/w/projects",
    settings: () => "/w/settings",
  }),
}));

const viewer = vi.hoisted(() => ({ guest: false }));
vi.mock("@multica/core/permissions", () => ({ useIsGuest: () => viewer.guest }));

const onClose = vi.fn();

function renderSheet(pathname: string) {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname,
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <KeyboardShortcutsModal onClose={onClose} />
    </NavigationProvider>,
  );
  return adapter;
}

const headings = () => screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent);
const group = (name: string) => screen.getByRole("region", { name: new RegExp(`^${name}`) });

describe("KeyboardShortcutsModal", () => {
  beforeEach(() => {
    configureShortcutPlatform("macos");
    onClose.mockClear();
    viewer.guest = false;
  });

  afterEach(() => {
    configureShortcutPlatform(null);
    act(() => useShortcutStore.getState().resetAll());
  });

  it("leads with the Inbox and issue keys on the Inbox", () => {
    renderSheet("/w/inbox");
    expect(headings()).toEqual([
      "Inbox · current page",
      "Issue · current page",
      "Navigation",
      "General",
      "Lists and boards",
      "Editor",
    ]);
  });

  it("leads with the list keys on an issue list", () => {
    renderSheet("/w/projects/p-1");
    expect(headings().slice(0, 2)).toEqual(["Lists and boards · current page", "Issue · current page"]);
  });

  it("marks nothing as current elsewhere", () => {
    renderSheet("/w/agents");
    expect(headings()[0]).toBe("Inbox");
  });

  it("shows each binding as it is set, sequences included, and leaves out unbound actions", () => {
    act(() => useShortcutStore.getState().setShortcut("createIssue", createShortcutChord("N")));
    renderSheet("/w/agents");

    const navigation = group("Navigation");
    expect(within(navigation).getByRole("img", { name: "G I" })).toBeInTheDocument();
    expect(within(navigation).getAllByText("then").length).toBeGreaterThan(0);
    expect(within(navigation).queryByText("Go to Autopilot")).not.toBeInTheDocument();
    expect(within(group("General")).getByRole("img", { name: "N" })).toBeInTheDocument();
    expect(within(group("Editor")).queryByText("Voice input")).not.toBeInTheDocument();
  });

  it("leaves out what a guest cannot do", () => {
    viewer.guest = true;
    renderSheet("/w/inbox");
    expect(screen.queryByText("Go to Chat")).not.toBeInTheDocument();
    expect(screen.queryByText("Create issue")).not.toBeInTheDocument();
    expect(screen.queryByText("Change status")).not.toBeInTheDocument();
    expect(screen.queryByText("Toggle chat window")).not.toBeInTheDocument();
    expect(screen.queryByText("Ask Multica")).not.toBeInTheDocument();
    expect(screen.getByText("Go to Inbox")).toBeInTheDocument();
    expect(screen.getByText("Reply")).toBeInTheDocument();
  });

  it("lists the fixed list keys", () => {
    renderSheet("/w/issues");
    const list = group("Lists and boards");
    expect(within(list).getByText("Select")).toBeInTheDocument();
    expect(within(list).getByRole("img", { name: "X" })).toBeInTheDocument();
    expect(within(list).getByRole("img", { name: "⌘A" })).toBeInTheDocument();
  });

  it("filters by name", () => {
    renderSheet("/w/inbox");
    fireEvent.change(screen.getByRole("textbox", { name: "Search actions..." }), { target: { value: "status" } });
    expect(headings()).toEqual(["Issue · current page"]);

    fireEvent.change(screen.getByRole("textbox", { name: "Search actions..." }), { target: { value: "zzz" } });
    expect(screen.getByText("No matching shortcut actions.")).toBeInTheDocument();
  });

  it("links to the Shortcuts settings, closing the sheet", () => {
    const adapter = renderSheet("/w/inbox");
    fireEvent.click(screen.getByRole("link", { name: "Settings › Shortcuts" }));
    expect(onClose).toHaveBeenCalled();
    expect(adapter.push).toHaveBeenCalledWith("/w/settings?tab=shortcuts");
  });
});
