import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { InboxItem } from "@multica/core/types";

const runMock = vi.fn().mockResolvedValue({ id: "n1", status: "queued" });

vi.mock("@multica/core/api", () => ({
  api: { runPrivateAgentRunRequest: (id: string) => runMock(id) },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Alice" }),
}));

import { CerebroInboxRunRequestRow } from "@multica/cerebro-inbox";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function makeItem(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "n1",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "owner",
    actor_type: "member",
    actor_id: "alice",
    type: "private_agent_run_request",
    severity: "action_required",
    route: "inbox",
    issue_id: "i1",
    project_id: null,
    title: "Designer Agent",
    body: null,
    issue_status: "todo",
    read: false,
    archived: false,
    muted_until: null,
    created_at: new Date().toISOString(),
    details: { agent_id: "a1", comment_id: "c1", requester_id: "alice" },
    ...overrides,
  };
}

describe("CerebroInboxRunRequestRow", () => {
  beforeEach(() => runMock.mockClear());

  it("renders nothing for a non run-request row", () => {
    const { container } = render(
      <CerebroInboxRunRequestRow item={makeItem({ type: "new_comment" })} />,
      { wrapper },
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the run-request badge, requester, and a Run button", () => {
    render(<CerebroInboxRunRequestRow item={makeItem()} />, { wrapper });
    expect(screen.getByText("Run request")).toBeInTheDocument();
    expect(screen.getByText((t) => t.includes("Alice"))).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run" })).toBeInTheDocument();
  });

  it("calls the run endpoint with the item id when Run is clicked", async () => {
    render(<CerebroInboxRunRequestRow item={makeItem()} />, { wrapper });
    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    await waitFor(() => expect(runMock).toHaveBeenCalledWith("n1"));
  });
});
