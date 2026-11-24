// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "@multica/core/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }),
}));
const relationMocks = vi.hoisted(() => ({
  removeBlocker: vi.fn(),
  removeBlocking: vi.fn(),
  openModal: vi.fn(),
  duplicates: [] as unknown[],
}));
vi.mock("@multica/core/issues/blockers", () => ({
  useUpdateIssueBlockers: () => ({ mutate: relationMocks.removeBlocker, isPending: false, isError: false, error: null }),
  useUpdateIssueBlocking: () => ({ mutate: relationMocks.removeBlocking, isPending: false, isError: false, error: null }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  issueDuplicatesOptions: () => ({ queryKey: ["duplicates"], queryFn: async () => ({ duplicate_of: null, duplicates: relationMocks.duplicates }) }),
}));
vi.mock("@multica/core/modals", () => {
  const state = { open: relationMocks.openModal };
  return { useModalStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), { getState: () => state }) };
});
vi.mock("../utils/status-options", () => ({ useIssueStatusOptions: () => ({ options: [], workflow: null }) }));
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, className }: { children: React.ReactNode; href: string; className?: string }) => <a href={href} className={className}>{children}</a>,
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ categoryOf: () => "unstarted", colorOf: () => null, iconOf: () => null }),
}));
const directory = vi.hoisted(() => ({
  agents: [
    { id: "agent-1", runtime_id: "runtime-1", archived_at: null },
    { id: "agent-archived", runtime_id: "runtime-1", archived_at: "2026-11-01T00:00:00Z" },
    { id: "agent-no-runtime", runtime_id: "", archived_at: null },
  ],
  squads: [
    { id: "squad-1", leader_id: "agent-1" },
    { id: "squad-idle", leader_id: "agent-archived" },
  ],
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => directory.agents }),
  squadListOptions: () => ({ queryKey: ["squads"], queryFn: async () => directory.squads }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lambda" }),
}));

import { IssueBlockedMarker, IssueRelationsSection } from "./issue-blockers";

afterEach(() => {
  cleanup();
  relationMocks.removeBlocker.mockReset();
  relationMocks.removeBlocking.mockReset();
  relationMocks.openModal.mockReset();
  relationMocks.duplicates = [];
});

function renderSection(issue: Issue) {
  return renderWithI18n(<QueryClientProvider client={new QueryClient()}><IssueRelationsSection issue={issue} /></QueryClientProvider>);
}

function waitingIssue(status: string): Issue {
  return {
    id: "issue-1",
    status,
    parent_issue_id: null,
    blocked_count: 1,
    blocked_by: [{ id: "b-1", identifier: "TEST-1", title: "Prerequisite", status: "todo", via: "manual", closed: false }],
  } as unknown as Issue;
}

describe("IssueRelationsSection hint", () => {
  it("promises an automatic start for an issue outside Backlog", () => {
    renderSection(waitingIssue("todo"));
    expect(screen.getByText(/starts automatically or receives a notification/)).toBeInTheDocument();
    expect(screen.queryByText(/is in Backlog/)).not.toBeInTheDocument();
  });

  it("says a Backlog issue stays parked after its blockers end", () => {
    renderSection(waitingIssue("backlog"));
    expect(screen.getByText(/is in Backlog, so it won't start automatically/)).toBeInTheDocument();
    expect(screen.queryByText(/starts automatically or receives a notification/)).not.toBeInTheDocument();
  });

  it("drops the start hint once the issue itself is finished", () => {
    renderSection(waitingIssue("done"));
    expect(screen.getByText("Prerequisite")).toBeInTheDocument();
    expect(screen.queryByText(/starts automatically or receives a notification/)).not.toBeInTheDocument();
    expect(screen.queryByText(/is in Backlog/)).not.toBeInTheDocument();
  });
});

describe("IssueRelationsSection", () => {
  const ref = (id: string, title: string, extra: Record<string, unknown> = {}) =>
    ({ id, identifier: id.toUpperCase(), title, status: "todo", via: "manual", closed: false, ...extra });

  it("lists only unfinished blockers and counts the ended ones", () => {
    renderSection({
      id: "issue-1",
      status: "todo",
      blocked_count: 2,
      blocked_by: [
        ref("t-1", "Open prerequisite"),
        ref("t-2", "Finished prerequisite", { status: "done", closed: true }),
        ref("t-3", "Earlier stage", { via: "stage" }),
      ],
      blocking: [],
    } as unknown as Issue);
    expect(screen.getByText("Blocked by")).toBeInTheDocument();
    expect(screen.getByText("1/3 ended")).toBeInTheDocument();
    expect(screen.getByText("Open prerequisite")).toBeInTheDocument();
    expect(screen.queryByText("Finished prerequisite")).not.toBeInTheDocument();
    // A stage edge belongs to the parent's stages: tagged, and not removable here.
    expect(screen.getByText("Stage")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove T-3" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove T-1" }));
    expect(relationMocks.removeBlocker).toHaveBeenCalledWith({ remove_blocked_by: ["t-1"] });
  });

  it("lists the unfinished issues this one blocks and removes the edge on the other issue", () => {
    renderSection({
      id: "issue-1",
      status: "todo",
      blocked_count: 0,
      blocked_by: [],
      blocking: [ref("d-1", "Waiting dependent"), ref("d-2", "Finished dependent", { status: "done", closed: true })],
    } as unknown as Issue);
    expect(screen.getByText("Blocking")).toBeInTheDocument();
    expect(screen.queryByText("Blocked by")).not.toBeInTheDocument();
    expect(screen.queryByText("Finished dependent")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove D-1" }));
    expect(relationMocks.removeBlocking).toHaveBeenCalledWith({ dependentId: "d-1", remove: true });
  });

  it("lists duplicates", async () => {
    relationMocks.duplicates = [{ id: "dup-1", identifier: "TEST-9", title: "Same bug again", status: "cancelled" }];
    renderSection({ id: "issue-1", status: "todo", blocked_count: 0, blocked_by: [], blocking: [] } as unknown as Issue);
    expect(await screen.findByText("Same bug again")).toBeInTheDocument();
    expect(screen.getByText("Duplicates")).toBeInTheDocument();
  });

  it("renders nothing when there is no relation to list", () => {
    const { container } = renderSection({
      id: "issue-1",
      status: "todo",
      blocked_count: 0,
      blocked_by: [ref("t-2", "Finished prerequisite", { status: "done", closed: true })],
      blocking: [],
    } as unknown as Issue);
    expect(container).toBeEmptyDOMElement();
  });

  it("asks for the relation type before opening a picker", async () => {
    renderSection(waitingIssue("todo"));
    fireEvent.click(screen.getByRole("button", { name: "Add relation" }));
    expect(await screen.findByRole("menuitem", { name: /Wait for another task/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /Mark as duplicate/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitem", { name: /Block another task/ }));
    expect(relationMocks.openModal).toHaveBeenCalledWith("issue-add-blocking", { issueId: "issue-1" });
  });
});

describe("IssueBlockedMarker", () => {
  it("flags a waiting issue and stays hidden once the issue is finished", () => {
    const { rerender } = renderWithI18n(<IssueBlockedMarker issue={waitingIssue("todo")} />);
    expect(screen.getByLabelText(/Waiting for 1 task/i)).toBeInTheDocument();
    rerender(<IssueBlockedMarker issue={waitingIssue("done")} />);
    expect(screen.queryByLabelText(/Waiting for/i)).not.toBeInTheDocument();
  });
});

describe("IssueBlockedMarker hover card", () => {
  function blockedIssue(overrides: Partial<Issue> = {}): Issue {
    return {
      id: "issue-1",
      status: "todo",
      assignee_type: "agent",
      assignee_id: "agent-1",
      blocked_count: 1,
      blocked_by: [
        { id: "b-2", identifier: "TEST-2", title: "Finished prerequisite", status: "done", via: "manual", closed: true },
        { id: "b-1", identifier: "TEST-1", title: "Open prerequisite", status: "in_progress", via: "manual", closed: false },
      ],
      ...overrides,
    } as unknown as Issue;
  }

  async function hover(issue: Issue) {
    renderWithI18n(<QueryClientProvider client={new QueryClient()}><IssueBlockedMarker issue={issue} delay={0} /></QueryClientProvider>);
    fireEvent.mouseEnter(screen.getByLabelText("Waiting for 1 task"));
    fireEvent.pointerEnter(screen.getByLabelText("Waiting for 1 task"));
    return screen.findByText("Blocked by 1 task");
  }

  it("names the blockers, unfinished first, and who starts afterwards", async () => {
    await hover(blockedIssue());
    expect(screen.getByText("· 1/2 ended", { exact: false })).toBeInTheDocument();
    const links = screen.getAllByRole("link").map((a) => a.getAttribute("href"));
    expect(links).toEqual(["/issues/TEST-1", "/issues/TEST-2"]);
    expect(await screen.findByText("Lambda starts automatically once these end.")).toBeInTheDocument();
  });

  it("says nothing starts when the agent, or a squad's leader, cannot run", async () => {
    for (const [assignee_type, assignee_id] of [["agent", "agent-archived"], ["agent", "agent-no-runtime"], ["agent", "agent-deleted"], ["squad", "squad-idle"]]) {
      await hover(blockedIssue({ assignee_type, assignee_id } as Partial<Issue>));
      expect(await screen.findByText("Lambda can't run right now, so nothing starts when these end.")).toBeInTheDocument();
      cleanup();
    }
    await hover(blockedIssue({ assignee_type: "squad", assignee_id: "squad-1" }));
    expect(await screen.findByText("Lambda starts automatically once these end.")).toBeInTheDocument();
  });

  it("says a member assignee is notified", async () => {
    await hover(blockedIssue({ assignee_type: "member", assignee_id: "user-1" }));
    expect(screen.getByText("Lambda is notified once these end.")).toBeInTheDocument();
  });

  it("says nothing starts without an assignee, and Backlog stays parked", async () => {
    await hover(blockedIssue({ assignee_type: null, assignee_id: null }));
    expect(screen.getByText("No assignee, so nothing starts when these end.")).toBeInTheDocument();
    cleanup();
    await hover(blockedIssue({ status: "backlog" }));
    expect(screen.getByText(/is in Backlog, so it won't start automatically/)).toBeInTheDocument();
  });

  it("counts blockers the viewer cannot see", async () => {
    await hover(blockedIssue({ blocked_count: 1, blocked_by: [] }));
    expect(screen.getByText("1 restricted prerequisites")).toBeInTheDocument();
  });
});
