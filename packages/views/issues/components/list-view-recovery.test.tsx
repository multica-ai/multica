// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { getIssueSurfaceViewStore, pruneIssueSurfaceViewStates } from "@multica/core/issues/stores/surface-view-store";
import type { Agent, Issue, IssueTableRowsRequest } from "@multica/core/types";
import { AgentWorkDialog } from "../../agents/components/agent-work-dialog";
import { NavigationProvider } from "../../navigation";
import { RESOURCES } from "../../locales";

// jsdom has no viewport measurements. Keep the real list and footer wiring.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({ data, itemContent, components }: any) => <>
    {data.map((item: Issue, index: number) => <div key={item.id}>{itemContent(index, item)}</div>)}
    {components?.Footer && <components.Footer />}
  </>,
}));

const wsId = "00000000-0000-4000-8000-000000000001";
const actorId = "00000000-0000-4000-8000-000000000002";
const surfaceKey = `actor:agent:${actorId}:assigned`;
const issue: Issue = {
  id: "00000000-0000-4000-8000-000000000003", workspace_id: wsId,
  number: 1, identifier: "TEST-1", title: "Synthetic assigned issue",
  description: null, status: "todo", priority: "none", position: 1,
  assignee_type: "agent", assignee_id: actorId, creator_type: "member", creator_id: actorId,
  parent_issue_id: null, project_id: null, stage: null, start_date: null, due_date: null,
  labels: [], metadata: {}, properties: {},
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

describe("ListView row-page recovery through the agent work dialog", () => {
  let qc: QueryClient;
  let api: ApiClient;
  let fail: boolean;
  let total: number;
  let pending: boolean;
  let rows: ReturnType<typeof vi.fn<ApiClient["listIssueTableRows"]>>;

  beforeEach(() => {
    fail = false; total = 1; pending = false;
    qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    qc.setQueryData(workspaceKeys.list(), [{ id: wsId, slug: "synthetic", name: "Synthetic" }]);
    api = new ApiClient("http://unused.invalid");
    // All server mocks are at the API boundary; production query hooks remain mounted.
    vi.spyOn(api, "listWorkspaces").mockResolvedValue([{ id: wsId, slug: "synthetic", name: "Synthetic" }] as never);
    vi.spyOn(api, "listIssueStatuses").mockResolvedValue({ statuses: [], categories: ["unstarted", "started", "done", "closed"], total: 0 });
    vi.spyOn(api, "listProperties").mockResolvedValue({ properties: [], total: 0 });
    vi.spyOn(api, "listProjects").mockResolvedValue({ projects: [], total: 0 });
    vi.spyOn(api, "listIssueWakeupSummaries").mockResolvedValue([]);
    vi.spyOn(api, "listPausedWakeups").mockResolvedValue([]);
    vi.spyOn(api, "listIssueWorkflows").mockResolvedValue({ workflows: [], total: 0 });
    vi.spyOn(api, "listLabels").mockResolvedValue({ labels: [], total: 0 });
    vi.spyOn(api, "listSquads").mockResolvedValue([]);
    vi.spyOn(api, "listMembers").mockResolvedValue([]);
    vi.spyOn(api, "listAgents").mockResolvedValue([]);
    vi.spyOn(api, "getAgentTaskSnapshot").mockResolvedValue([]);
    vi.spyOn(api, "getChildIssueProgress").mockResolvedValue({ progress: [] });
    vi.spyOn(api, "listIssueTableFacets").mockImplementation(async (request) => ({
      query_fingerprint: "synthetic", total,
      facets: request.facets.map((facet) => ({ ...facet, values: facet.kind === "status" && total ? [{ key: "todo", count: total }] : [] })),
    }));
    rows = vi.fn(async (request: IssueTableRowsRequest) => {
      if (request.group_key === "status:todo") {
        if (pending) return new Promise<never>(() => {});
        if (fail) throw new Error("Synthetic row page unavailable");
      }
      return {
        query_fingerprint: "synthetic", total, group_key: request.group_key ?? null,
        parent_id: null, branch_total: request.group_key === "status:todo" ? total : 0,
        rows: request.group_key === "status:todo" && total ? [{ issue, direct_child_count: 0 }] : [], next_cursor: null,
      };
    });
    vi.spyOn(api, "listIssueTableRows").mockImplementation(rows);
    setApiInstance(api);
    registerAuthStore(createAuthStore({ api, storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } }));
    pruneIssueSurfaceViewStates([]);
    getIssueSurfaceViewStore(surfaceKey).setState({ viewMode: "list", hiddenStatuses: [], listCollapsedStatuses: [] });
  });

  afterEach(() => { cleanup(); qc.clear(); pruneIssueSurfaceViewStates([]); vi.restoreAllMocks(); });

  function mount() {
    return render(<QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="synthetic">
        <I18nProvider locale="en" resources={RESOURCES}>
          <NavigationProvider value={{ pathname: "/synthetic/agents", searchParams: new URLSearchParams(), hash: "", push: () => {}, replace: () => {}, back: () => {}, getShareableUrl: (path) => path }}>
            <AgentWorkDialog agent={{ id: actorId, name: "Synthetic agent" } as Agent} open onOpenChange={() => {}} />
          </NavigationProvider>
        </I18nProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>);
  }

  it("loads a cold nonempty assigned list", async () => {
    mount();
    expect(await screen.findByText(issue.title)).toBeInTheDocument();
    expect(rows).toHaveBeenCalledWith(expect.objectContaining({ query: expect.objectContaining({ scope: { kind: "assignee", actor: { type: "agent", id: actorId } } }) }));
  });

  it("offers a working retry when the first row page fails despite nonzero facets", async () => {
    fail = true; mount();
    const retry = await screen.findByRole("button", { name: /could not load.*retry/i });
    const failedRequest = rows.mock.calls.find(([request]) => request.group_key === "status:todo");
    expect(screen.queryByText(issue.title)).not.toBeInTheDocument();
    fail = false;
    fireEvent.click(retry);
    expect(await screen.findByText(issue.title)).toBeInTheDocument();
    expect(rows.mock.calls.filter(([request]) => request.group_key === "status:todo")).toEqual([failedRequest, failedRequest]);
    await waitFor(() => expect(screen.queryByRole("button", { name: /could not load.*retry/i })).not.toBeInTheDocument());
  });

  it("keeps authoritative zero results empty", async () => {
    total = 0; mount();
    expect(await screen.findByText("No assigned issues")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /could not load.*retry/i })).not.toBeInTheDocument();
  });

  it("keeps the initial pending page in its loading state", async () => {
    pending = true; mount();
    await waitFor(() => expect(rows).toHaveBeenCalled());
    expect(document.querySelector('[data-slot="skeleton"]')).toBeInTheDocument();
    expect(screen.queryByText("No assigned issues")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /could not load.*retry/i })).not.toBeInTheDocument();
  });
});
