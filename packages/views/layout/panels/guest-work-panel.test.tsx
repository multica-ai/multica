import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { projectListOptions } from "@multica/core/projects/queries";
import type { InboxItem, Issue, Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { GuestWorkPanel } from "./guest-work-panel";

const apiMock = vi.hoisted(() => ({ getIssue: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
  };
});
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useIsGuest: () => true,
}));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  return { useIssueStatuses: () => buildIssueStatusCatalog([]) };
});

const ISSUE = {
  id: "0a3f0a8e-6a52-4c1e-9f5e-1b2c3d4e5f60",
  identifier: "MUL-1",
  title: "Review the brief",
  status: "todo",
} as Issue;
// An Inbox row's link also reads out who did what, after the title.
const INBOX_ROW_NAME = new RegExp(`^${ISSUE.title}`);

const PROJECT = {
  id: "5d4c3b2a-1f0e-4d9c-8b7a-6f5e4d3c2b1a",
  title: "Launch",
  status: "in_progress",
  issue_count: 0,
  done_count: 0,
  created_at: "",
  updated_at: "",
} as unknown as Project;

const NOTIFICATION: InboxItem = {
  id: `inbox-${ISSUE.id}`,
  workspace_id: "ws-1",
  recipient_type: "member",
  recipient_id: "guest-1",
  actor_type: "member",
  actor_id: "member-1",
  type: "mentioned",
  severity: "info",
  issue_id: ISSUE.id,
  title: ISSUE.title,
  body: null,
  issue_status: ISSUE.status,
  issue_priority: "none",
  read: false,
  archived: false,
  created_at: "2026-11-10T08:00:00Z",
  details: null,
};

function renderPanel(initialPath: string, inbox: InboxItem[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(inboxListOptions("ws-1").queryKey, inbox);
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects: [PROJECT], total: 1 });
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);

  function Harness() {
    const [pathname, setPathname] = useState(initialPath);
    const adapter: NavigationAdapter = {
      push: setPathname,
      replace: setPathname,
      back: vi.fn(),
      pathname: pathname.split("?")[0]!,
      searchParams: new URLSearchParams(pathname.split("?")[1]),
      hash: "",
      getShareableUrl: (path) => path,
    };
    return (
      <NavigationProvider value={adapter}>
        <GuestWorkPanel />
        <output data-testid="pathname">{pathname}</output>
      </NavigationProvider>
    );
  }

  renderWithI18n(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
}

const inInbox = () => document.querySelector('[data-sidebar-list="inbox"]') as HTMLElement;

beforeEach(() => {
  useSidebarTreeStore.setState({ inboxCollapsed: false });
  useInboxSelectionStore.setState({
    heldRows: {},
    leftRowKeyByWorkspace: {},
  });
  // The issue page never finishes loading, so the panel cannot lean on it.
  apiMock.getIssue.mockReturnValue(new Promise<Issue>(() => {}));
});

describe("GuestWorkPanel", () => {
  it("lists the guest's notifications beside an Inbox page that shows only the open one", async () => {
    renderPanel("/acme/inbox", [NOTIFICATION]);
    const labels = Array.from(document.querySelectorAll("[data-mc='panel-section'] h3")).map((h) => h.textContent);
    expect(labels).toEqual(["Inbox", "Shared with you"]);
    expect(await within(inInbox()).findByRole("link", { name: INBOX_ROW_NAME })).toBeInTheDocument();
  });

  it("opens a notification on the Inbox page and keeps its row lit", async () => {
    renderPanel("/acme/inbox", [NOTIFICATION]);
    fireEvent.click(await within(inInbox()).findByRole("link", { name: INBOX_ROW_NAME }));
    expect(screen.getByTestId("pathname")).toHaveTextContent(`/acme/inbox?issue=${ISSUE.id}`);
    await waitFor(() =>
      expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).toHaveAttribute("aria-current", "page"),
    );
  });
});
