import { cloneElement, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask, Issue, ListIssuesParams } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../../issues/actions";
import { SidebarIssueList } from "./sidebar-tree";

vi.mock("../../issues/actions/issue-actions-menu-items", () => ({
  contextPrimitives: {},
  IssueActionsMenuItems: ({ issue, variant }: { issue: Issue; variant?: string }) => (
    <div>{`${variant} menu: ${issue.title}`}</div>
  ),
}));
const updateField = vi.hoisted(() => vi.fn());
vi.mock("../../issues/actions/use-issue-actions", () => ({ useIssueActions: () => ({ updateField }) }));
vi.mock("../../issues/components/pickers/status-picker", () => ({
  StatusPicker: ({
    open,
    onUpdate,
    trigger,
    triggerRender,
  }: {
    open: boolean;
    onUpdate: (updates: { status: string }) => void;
    trigger: ReactNode;
    triggerRender: React.ReactElement;
  }) => (
    <>
      {cloneElement(triggerRender, {}, trigger)}
      {open && (
        <button type="button" onClick={() => onUpdate({ status: "done" })}>
          Pick Done
        </button>
      )}
    </>
  ),
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({
    children,
    href,
    newTabTitle: _newTabTitle,
    ...props
  }: { children: ReactNode; href: string; newTabTitle?: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({ agentIds }: { agentIds: string[] }) => <span data-testid="agents">{agentIds.join(",")}</span>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});

const SOURCE = "project:p1";
const PARAMS: ListIssuesParams = { project_id: "p1", sort_by: "updated_at", sort_direction: "desc" };
const issues = (count: number): Issue[] =>
  Array.from({ length: count }, (_, i) => ({ id: `issue-${i + 1}`, title: `Issue ${i + 1}`, status: "todo" }) as Issue);

function renderList({
  total = 18,
  params = PARAMS,
  tasks = [],
  onCollapse = vi.fn(),
}: {
  total?: number;
  params?: ListIssuesParams | null;
  tasks?: Partial<AgentTask>[];
  onCollapse?: () => void;
} = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  const all = issues(total);
  for (const limit of [5, 15, 25]) {
    qc.setQueryData(issueKeys.sidebar("ws-1", SOURCE, PARAMS, limit), { issues: all.slice(0, limit), total });
  }
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, tasks as AgentTask[]);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueContextMenuProvider variant="sidebar">
        <SidebarIssueList
          id="list"
          wsId="ws-1"
          source={SOURCE}
          params={params}
          activeIssueId="issue-2"
          onCollapse={onCollapse}
        />
      </IssueContextMenuProvider>
    </QueryClientProvider>,
  );
  return { onCollapse };
}

const rows = () => screen.getAllByRole("link");

describe("SidebarIssueList", () => {
  it("loads more in steps, then shows less", () => {
    renderList();
    expect(rows()).toHaveLength(5);
    expect(screen.getByRole("link", { name: "Issue 2" })).toHaveAttribute("aria-current", "page");

    fireEvent.click(screen.getByRole("button", { name: "Load more 13" }));
    expect(rows()).toHaveLength(15);
    fireEvent.click(screen.getByRole("button", { name: "Load more 3" }));
    expect(rows()).toHaveLength(18);

    fireEvent.click(screen.getByRole("button", { name: "Show less" }));
    expect(rows()).toHaveLength(5);
  });

  it("offers neither control when everything fits", () => {
    renderList({ total: 4 });
    expect(rows()).toHaveLength(4);
    expect(screen.queryByRole("button", { name: /Load more|Show less/ })).toBeNull();
  });

  it("shows the empty state when the settings match nothing", () => {
    renderList({ params: null });
    expect(screen.getByText("No issues")).toBeInTheDocument();
  });

  it("marks issues an agent is working on or queued for", () => {
    renderList({
      tasks: [
        { id: "t1", issue_id: "issue-1", agent_id: "a1", status: "running" },
        { id: "t2", issue_id: "issue-3", agent_id: "a2", status: "queued" },
      ],
    });
    const working = screen.getByRole("link", { name: /^Issue 1/ });
    expect(working).toHaveTextContent("Working");
    expect(working.querySelector(".shimmer-text")).not.toBeNull();
    const queued = screen.getByRole("link", { name: /^Issue 3/ });
    expect(queued).toHaveTextContent("Queued");
    expect(queued.querySelector(".shimmer-text")).toBeNull();
    expect(screen.getByRole("link", { name: "Issue 2" }).querySelector("[data-testid=agents]")).toBeNull();
  });

  it("collapses the row from the keyboard", () => {
    const { onCollapse } = renderList();
    fireEvent.keyDown(rows()[0]!, { key: "ArrowLeft" });
    expect(onCollapse).toHaveBeenCalled();
  });

  it("opens the sidebar issue menu on right-click and keeps the row lit while it is open", async () => {
    renderList();
    const row = screen.getByRole("link", { name: "Issue 3" }).parentElement!;
    const selected = screen.getByRole("link", { name: "Issue 2" }).parentElement!;
    fireEvent.contextMenu(row);

    expect(await screen.findByText("sidebar menu: Issue 3")).toBeInTheDocument();
    expect(row).toHaveAttribute("data-popup-open");
    expect(row.className).toContain("data-popup-open:bg-surface-hover");
    // The selected row keeps its own fill.
    expect(selected.className).not.toContain("data-popup-open:");
  });

  it("changes the status from the row's status icon without opening the issue", () => {
    updateField.mockReset();
    renderList({ total: 1 });
    const status = screen.getByRole("button", { name: "Todo" });
    expect(status.closest("a")).toBeNull();

    fireEvent.click(status);
    fireEvent.click(screen.getByRole("button", { name: "Pick Done" }));
    expect(updateField).toHaveBeenCalledWith({ status: "done" });
  });
});
