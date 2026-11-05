import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { InboxProjectLabel } from "./inbox-project-label";

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", name: "Acme" }),
}));
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: (wsId: string) => ({
    queryKey: ["projects", wsId, "list"],
    queryFn: async () => [{ id: "p-1", title: "江苏中行", icon: "📁" }],
  }),
}));

function item(over: Partial<InboxItem>): InboxItem {
  return {
    id: "i-1", workspace_id: "ws-1", recipient_type: "member", recipient_id: "u-1",
    actor_type: "agent", actor_id: "a-1", type: "new_comment", severity: "info",
    issue_id: "iss-1", title: "t", body: null, issue_status: null, read: false,
    archived: false, created_at: "2026-11-04T00:00:00Z", details: null, ...over,
  } as InboxItem;
}

function renderLabel(it: InboxItem) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><InboxProjectLabel item={it} /></QueryClientProvider>);
}

describe("InboxProjectLabel", () => {
  it("names the issue's project", async () => {
    renderLabel(item({ issue_project_id: "p-1" }));
    expect(await screen.findByText("江苏中行")).toBeInTheDocument();
  });

  it("renders nothing without a project", () => {
    const { container } = renderLabel(item({ issue_project_id: null }));
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for another workspace's item", () => {
    const { container } = renderLabel(item({ workspace_id: "ws-2", issue_project_id: "p-1" }));
    expect(container).toBeEmptyDOMElement();
  });
});
