import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { InboxSections } from "@multica/core/inbox/sections";
import type { AgentTask, InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InboxPeek } from "./list-space-peeks";

const push = vi.hoisted(() => vi.fn());
const archive = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ inbox: () => "/acme/inbox", chat: () => "/acme/chat" }),
}));
vi.mock("@multica/core/inbox/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/queries")>()),
  useInboxUnreadCount: () => 1,
}));
vi.mock("@multica/core/inbox/mutations", () => ({ useArchiveInbox: () => ({ mutate: archive }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push }) }));
vi.mock("../../chat/components/chat-thread-list", () => ({ ChatThreadList: () => null }));

// The list itself has its own tests. Here it is the sections it was handed
// and the callbacks the peek wired into it.
vi.mock("../../inbox/components/inbox-sectioned-list", () => ({
  InboxSectionedList: ({
    sections,
    filtered,
    scrollRestoration,
    onSelect,
    onSelectIssue,
    onAction,
  }: {
    sections: InboxSections;
    filtered: boolean;
    scrollRestoration: boolean;
    onSelect: (item: InboxItem) => void;
    onSelectIssue: (issueId: string) => void;
    onAction: (id: string) => void;
  }) => (
    <div data-testid="list" data-filtered={String(filtered)} data-scroll-restoration={String(scrollRestoration)}>
      {(["needsYou", "updates", "snoozed"] as const).flatMap((section) =>
        sections[section].map((row) => (
          <span key={row.item.id}>
            <button type="button" data-row={section} onClick={() => onSelect(row.item)}>
              {row.item.id}
            </button>
            <button type="button" onClick={() => onAction(row.item.id)}>
              archive {row.item.id}
            </button>
          </span>
        )),
      )}
      {sections.working.map((row) => (
        <button key={row.issueId} type="button" data-row="working" onClick={() => onSelectIssue(row.issueId)}>
          {row.issueId}
        </button>
      ))}
    </div>
  ),
}));

function item(id: string, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id,
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: `issue-${id}`,
    title: "Issue title",
    body: null,
    issue_status: null,
    read: true,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function renderPeek(items: InboxItem[], tasks: AgentTask[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(inboxKeys.list("ws-1"), items);
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, tasks);
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <InboxPeek />
    </QueryClientProvider>,
  );
}

const rows = () =>
  Array.from(document.querySelectorAll("[data-row]")).map(
    (el) => `${el.getAttribute("data-row")}:${el.textContent}`,
  );

beforeEach(() => {
  push.mockClear();
  archive.mockClear();
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
});

describe("InboxPeek", () => {
  it("shows the Inbox in its sections, without the archived or the snoozed mixed in", () => {
    renderPeek(
      [
        item("needs", { section: "needs_you" }),
        item("update", { section: "updates", read: false }),
        // Read, so on its way to the archive.
        item("seen", { section: "updates" }),
        item("later", { section: "needs_you", snoozed_until: new Date(Date.now() + 3_600_000).toISOString() }),
        item("gone", { section: "needs_you", archived: true }),
      ],
      [
        {
          id: "task-1",
          agent_id: "agent-1",
          issue_id: "issue-w",
          status: "running",
          created_at: "2026-06-15T08:00:00Z",
          started_at: "2026-06-15T08:01:00Z",
          attribution: { source: "direct_human", precise: true, originator: { id: "member-1" } },
        } as AgentTask,
      ],
    );

    expect(rows()).toEqual(["needsYou:needs", "updates:update", "snoozed:later", "working:issue-w"]);
    // A preview over another route must not take that route's scroll offset.
    expect(screen.getByTestId("list")).toHaveAttribute("data-scroll-restoration", "false");
  });

  it("lands on the row in the Inbox, whether it is a notification or a Working issue", () => {
    renderPeek([item("needs", { section: "needs_you" })], [
      {
        id: "task-1",
        agent_id: "agent-1",
        issue_id: "issue-w",
        status: "queued",
        created_at: "2026-06-15T08:00:00Z",
        attribution: { source: "direct_human", precise: true, originator: { id: "member-1" } },
      } as AgentTask,
    ]);

    fireEvent.click(screen.getByText("needs"));
    expect(push).toHaveBeenLastCalledWith("/acme/inbox?issue=issue-needs");
    fireEvent.click(screen.getByText("issue-w"));
    expect(push).toHaveBeenLastCalledWith("/acme/inbox?issue=issue-w");
  });

  it("archives from the row", () => {
    renderPeek([item("needs", { section: "needs_you" })]);

    fireEvent.click(screen.getByText("archive needs"));

    expect(archive).toHaveBeenCalledWith("needs");
  });

  it("follows the Inbox's saved filters, Working included", async () => {
    setApiInstance({
      getIssue: async (id: string) => ({ id, status: "todo", priority: id === "issue-w" ? "high" : "low", labels: [] }),
    } as unknown as ApiClient);
    const filters = useInboxFilterStore.getState();
    filters.togglePriorityFilter("ws-1", "high");
    // A filter for the archive, which the Inbox leaves alone.
    filters.toggleUnreadOnly("ws-1");
    const run = (issueId: string) =>
      ({
        id: `task-${issueId}`,
        agent_id: "agent-1",
        issue_id: issueId,
        status: "running",
        created_at: "2026-06-15T08:00:00Z",
        attribution: { source: "direct_human", precise: true, originator: { id: "member-1" } },
      }) as AgentTask;
    renderPeek(
      [
        item("high", { section: "needs_you", issue_priority: "high" }),
        item("low", { section: "needs_you", issue_priority: "low", read: false }),
      ],
      [run("issue-w"), run("issue-other")],
    );

    await waitFor(() => expect(rows()).toEqual(["needsYou:high", "working:issue-w"]));
    expect(screen.getByTestId("list")).toHaveAttribute("data-filtered", "true");
  });

  it("keeps narrowing Working once no notification is left", async () => {
    setApiInstance({
      getIssue: async (id: string) => ({ id, status: "todo", project_id: id === "issue-a" ? "project-a" : "project-b", labels: [] }),
    } as unknown as ApiClient);
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "project", "project-a");
    const run = (issueId: string) =>
      ({
        id: `task-${issueId}`,
        agent_id: "agent-1",
        issue_id: issueId,
        status: "running",
        created_at: "2026-06-15T08:00:00Z",
        attribution: { source: "direct_human", precise: true, originator: { id: "member-1" } },
      }) as AgentTask;
    renderPeek([], [run("issue-a"), run("issue-b")]);

    await waitFor(() => expect(rows()).toEqual(["working:issue-a"]));
    expect(screen.getByTestId("list")).toHaveAttribute("data-filtered", "true");
  });
});
