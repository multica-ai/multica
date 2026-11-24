// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }),
}));
vi.mock("@multica/core/issues/blockers", () => ({
  useUpdateIssueBlockers: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false, isError: false }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, className }: { children: React.ReactNode; href: string; className?: string }) => <a href={href} className={className}>{children}</a>,
}));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({ categoryOf: () => "unstarted", colorOf: () => null, iconOf: () => null }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lambda" }),
}));

import { IssueBlockedMarker, IssueBlockersSection } from "./issue-blockers";

afterEach(cleanup);

function waitingIssue(status: string): Issue {
  return {
    id: "issue-1",
    status,
    parent_issue_id: null,
    blocked_count: 1,
    blocked_by: [{ id: "b-1", identifier: "TEST-1", title: "Prerequisite", status: "todo", via: "manual", closed: false }],
  } as unknown as Issue;
}

describe("IssueBlockersSection hint", () => {
  it("promises an automatic start for an issue outside Backlog", () => {
    renderWithI18n(<IssueBlockersSection issue={waitingIssue("todo")} />);
    expect(screen.getByText(/starts automatically or receives a notification/)).toBeInTheDocument();
    expect(screen.queryByText(/is in Backlog/)).not.toBeInTheDocument();
  });

  it("says a Backlog issue stays parked after its blockers end", () => {
    renderWithI18n(<IssueBlockersSection issue={waitingIssue("backlog")} />);
    expect(screen.getByText(/is in Backlog, so it won't start automatically/)).toBeInTheDocument();
    expect(screen.queryByText(/starts automatically or receives a notification/)).not.toBeInTheDocument();
  });

  it("drops the start hint once the issue itself is finished", () => {
    renderWithI18n(<IssueBlockersSection issue={waitingIssue("done")} />);
    expect(screen.getByText("Prerequisite")).toBeInTheDocument();
    expect(screen.queryByText(/starts automatically or receives a notification/)).not.toBeInTheDocument();
    expect(screen.queryByText(/is in Backlog/)).not.toBeInTheDocument();
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
    renderWithI18n(<IssueBlockedMarker issue={issue} delay={0} />);
    fireEvent.mouseEnter(screen.getByLabelText("Waiting for 1 task"));
    fireEvent.pointerEnter(screen.getByLabelText("Waiting for 1 task"));
    return screen.findByText("Blocked by 1 task");
  }

  it("names the blockers, unfinished first, and who starts afterwards", async () => {
    await hover(blockedIssue());
    expect(screen.getByText("· 1/2 ended", { exact: false })).toBeInTheDocument();
    const links = screen.getAllByRole("link").map((a) => a.getAttribute("href"));
    expect(links).toEqual(["/issues/TEST-1", "/issues/TEST-2"]);
    expect(screen.getByText("Lambda starts automatically once these end.")).toBeInTheDocument();
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
