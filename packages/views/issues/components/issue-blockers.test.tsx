// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
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
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
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
});

describe("IssueBlockedMarker", () => {
  it("flags a waiting issue and stays hidden once the issue is finished", () => {
    const { rerender } = renderWithI18n(<IssueBlockedMarker issue={waitingIssue("todo")} />);
    expect(screen.getByLabelText(/Waiting for 1 task/i)).toBeInTheDocument();
    rerender(<IssueBlockedMarker issue={waitingIssue("done")} />);
    expect(screen.queryByLabelText(/Waiting for/i)).not.toBeInTheDocument();
  });
});
