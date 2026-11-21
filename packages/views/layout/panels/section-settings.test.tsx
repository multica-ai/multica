import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_SIDEBAR_INBOX_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import type { IssueViewActions } from "../../issue-views/use-view-actions";
import { InboxSectionSettings, PinnedSectionSettings, ViewsSectionSettings } from "./sidebar-settings";

const preferences = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate: preferences.mutate }),
}));

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function render(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(notificationPreferenceOptions("ws-1").queryKey, {
    workspace_id: "ws-1",
    preferences: { comments: "muted" },
  });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>{ui}</NavigationProvider>
    </QueryClientProvider>,
  );
}

const toggle = (name: string) => screen.getByRole("switch", { name });

beforeEach(() => {
  preferences.mutate.mockReset();
  useSidebarTreeStore.setState({
    inboxSettings: DEFAULT_SIDEBAR_INBOX_SETTINGS,
    pinnedLimit: 5,
    viewsShowCount: true,
  });
});

describe("InboxSectionSettings", () => {
  function open() {
    render(<InboxSectionSettings />);
    fireEvent.click(screen.getByRole("button", { name: "Inbox display" }));
  }

  it("shows each notification kind as delivered unless muted, status changes off by default", () => {
    open();
    expect(toggle("Comments")).not.toBeChecked();
    expect(toggle("Mentions")).toBeChecked();
    expect(toggle("Status changes")).not.toBeChecked();
  });

  it("mutes a kind at write time through the notification preferences", () => {
    open();
    fireEvent.click(toggle("Mentions"));
    expect(preferences.mutate).toHaveBeenCalledWith({ comments: "muted", mentions: "muted" }, expect.anything());
  });

  it("turns a kind back on", () => {
    open();
    fireEvent.click(toggle("Comments"));
    expect(preferences.mutate).toHaveBeenCalledWith({ comments: "all" }, expect.anything());
  });

  it("turns status changes on", () => {
    open();
    fireEvent.click(toggle("Status changes"));
    expect(preferences.mutate).toHaveBeenCalledWith(
      { comments: "muted", status_changes: "all" },
      expect.anything(),
    );
  });

  it("turns off the section count and the Work badge", () => {
    open();
    fireEvent.click(toggle("Unread count"));
    fireEvent.click(toggle("Unread badge on Work"));
    expect(useSidebarTreeStore.getState().inboxSettings).toEqual({
      ...DEFAULT_SIDEBAR_INBOX_SETTINGS,
      showCount: false,
      railBadge: false,
    });
  });

  it("links to the full notification settings", () => {
    open();
    expect(screen.getByRole("link", { name: "All notification settings" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=notifications",
    );
  });
});

describe("PinnedSectionSettings", () => {
  it("offers a reset once the row limit moves off the default", () => {
    useSidebarTreeStore.setState({ pinnedLimit: 10 });
    render(<PinnedSectionSettings />);
    fireEvent.click(screen.getByRole("button", { name: "Pinned display" }));

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(useSidebarTreeStore.getState().pinnedLimit).toBe(5);
  });
});

describe("ViewsSectionSettings", () => {
  const sidebar = {
    isStarred: (id: string) => id === "builtin:my_work",
    star: vi.fn(),
    unstar: vi.fn(),
  };

  function open() {
    render(<ViewsSectionSettings sidebar={sidebar as unknown as IssueViewActions["sidebar"]} />);
    fireEvent.click(screen.getByRole("button", { name: "Views display" }));
  }

  it("stars and unstars the built-in views", () => {
    open();
    expect(toggle("My work")).toBeChecked();
    expect(toggle("All issues")).not.toBeChecked();
    expect(toggle("Working")).not.toBeChecked();
    expect(toggle("Subscribed")).not.toBeChecked();

    fireEvent.click(toggle("Subscribed"));
    expect(sidebar.star).toHaveBeenCalledWith("builtin:subscribed");
    fireEvent.click(toggle("My work"));
    expect(sidebar.unstar).toHaveBeenCalledWith("builtin:my_work");
  });

  it("turns the row counts off", () => {
    open();
    fireEvent.click(toggle("Counts"));
    expect(useSidebarTreeStore.getState().viewsShowCount).toBe(false);
  });
});
