import type { ReactNode } from "react";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { DashboardHistoryLimitParam } from "@multica/core/dashboard";
import type { ContributionSummary } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { ContributionTab } from "./contribution-tab";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
// Every list the tab does not assert on (projects) resolves empty.
vi.mock("@multica/core/api", () => {
  const fns: Record<string, ReturnType<typeof vi.fn>> = {};
  return {
    api: new Proxy(fns, {
      get: (target, key: string) => (target[key] ??= vi.fn().mockResolvedValue([])),
    }),
  };
});
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ m1: "Member One", m2: "Member Two", a1: "Coder Agent" })[id] ?? "Unknown",
  }),
}));
vi.mock("@multica/core/paths", () => ({
  issueRouteRef: (issue: { id: string; identifier: string | null }) => issue.identifier ?? issue.id,
  useWorkspacePaths: () => ({ issueDetail: (ref: string) => `/ws/issues/${ref}` }),
}));
vi.mock("../../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../../navigation", () => ({
  AppLink: ({ href, children, className }: { href: string; children: ReactNode; className?: string }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

const actor = (actor_type: string, actor_id: string, participated: number, completed = 0) => ({
  actor_type,
  actor_id,
  created: 0,
  commented: participated,
  status_changed: 0,
  completed,
  participated,
});

const summary: ContributionSummary = {
  kpis: {
    total: 412,
    active: 100,
    touched: 128,
    created: 81,
    created_by_member: 52,
    created_by_agent: 29,
    completed: 36,
    open: 85,
    open_in_progress: 61,
    open_in_review: 24,
  },
  daily: [{ date: "2026-11-18", active: 3, collaborated: 1, delivered: 1 }],
  weekly: [{ date: "2026-11-16", active: 3, collaborated: 1, delivered: 1 }],
  actors: [actor("member", "m1", 10, 2), actor("member", "m2", 4), actor("agent", "a1", 7, 5)],
  actor_weekly: [],
  system_completed: 3,
};

function renderTab(opts: { historyLimit?: DashboardHistoryLimitParam } = {}) {
  const historyLimit = "historyLimit" in opts ? opts.historyLimit : null;
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ContributionTab days={30} projectId={null} tz="UTC" historyLimit={historyLimit} />
    </QueryClientProvider>,
  );
}

describe("ContributionTab", () => {
  beforeEach(() => {
    vi.mocked(api.getContributionSummary).mockResolvedValue(summary);
    vi.mocked(api.getContributionEvents).mockResolvedValue({ events: [], total: 0 });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("lists issues by interactions and re-sorts from page one", async () => {
    const user = userEvent.setup();
    vi.mocked(api.getContributionIssues).mockResolvedValue({
      issues: [
        {
          issue_id: "i9",
          identifier: "MUL-9",
          title: "Busy issue",
          status: "in_progress",
          status_group: "in_progress",
          project_id: "",
          participants: 4,
          comments: 6,
          interactions: 11,
          completion_seconds: null,
        },
      ],
      total: 1,
    });
    renderTab();
    expect(await screen.findByText("Busy issue")).toBeTruthy();
    expect(screen.getByText("11")).toBeTruthy();
    expect(vi.mocked(api.getContributionIssues).mock.calls[0]![0]).toMatchObject({ sort: "interactions", offset: 0 });

    await user.click(screen.getByRole("button", { name: "Time to done" }));
    await waitFor(() =>
      expect(api.getContributionIssues).toHaveBeenLastCalledWith(expect.objectContaining({ sort: "completion", offset: 0 })),
    );
  });

  it("shows the KPIs and ranks people and agents separately", async () => {
    const user = userEvent.setup();
    renderTab();
    expect(await screen.findByText("412")).toBeTruthy();
    expect(screen.getByText("In progress 61 · In review 24")).toBeTruthy();
    expect(screen.getByText("3 issues completed by the system are not ranked")).toBeTruthy();
    expect(screen.getAllByText("Member One").length).toBeGreaterThan(0);
    expect(screen.queryByText("Coder Agent")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Agents" }));
    expect(screen.getAllByText("Coder Agent").length).toBeGreaterThan(0);
    expect(screen.queryByText("Member One")).toBeNull();
  });

  it("asks the activity list for the top person and agent by default", async () => {
    const user = userEvent.setup();
    renderTab();
    await screen.findByText("412");
    await user.click(screen.getByRole("button", { name: "Members" }));
    await waitFor(() => expect(api.getContributionEvents).toHaveBeenCalled());
    expect(vi.mocked(api.getContributionEvents).mock.calls[0]![0]).toMatchObject({
      actors: ["member:m1", "agent:a1"],
      offset: 0,
    });
  });
  it("waits for the plan's history limit before loading", async () => {
    renderTab({ historyLimit: undefined });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.getContributionSummary).not.toHaveBeenCalled();
  });

  it("shows the empty state when the workspace has no issue activity", async () => {
    vi.mocked(api.getContributionSummary).mockResolvedValue({
      ...summary,
      kpis: { ...summary.kpis, total: 0, touched: 0 },
    });
    renderTab();
    expect(await screen.findByText("No issue activity yet")).toBeTruthy();
  });
});
