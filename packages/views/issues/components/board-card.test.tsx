/**
 * @vitest-environment jsdom
 */
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { BoardCardContent } from "./board-card";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/properties", () => ({
  propertyListOptions: () => ({
    queryKey: ["properties", "ws-1"],
    queryFn: async () => [],
  }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({
      cardProperties: {
        priority: false,
        description: false,
        assignee: false,
        startDate: false,
        dueDate: false,
        project: false,
        childProgress: false,
        labels: false,
      },
      cardPropertyIds: [],
    }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => null }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (value: any) => string) =>
      selector({
        card: { unread_update: "Unread update" },
        priority: { none: "No priority" },
        pickers: { assignee: { trigger_unassigned: "Unassigned" } },
      }),
  }),
  useTimeAgo: () => () => "now",
}));

vi.mock("./issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: () => null,
}));

const issue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TEST-1",
  title: "Unread card",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "member-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  created_at: "2026-07-16T00:00:00Z",
  updated_at: "2026-07-16T00:00:00Z",
};

function renderCard(value: Issue) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <BoardCardContent issue={value} />
    </QueryClientProvider>,
  );
}

describe("BoardCardContent unread indicator", () => {
  it("shows an accessible dot only when the issue has unread updates", () => {
    const { rerender } = renderCard({ ...issue, has_unread: true });
    expect(screen.getByLabelText("Unread update")).toBeInTheDocument();

    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <BoardCardContent issue={{ ...issue, has_unread: false }} />
      </QueryClientProvider>,
    );
    expect(screen.queryByLabelText("Unread update")).not.toBeInTheDocument();
  });
});
