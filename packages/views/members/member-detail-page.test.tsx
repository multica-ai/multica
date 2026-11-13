import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { actorIssuesViewStore } from "@multica/core/issues/stores/actor-issues-view-store";
import type { Agent, MemberWithUser } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { renderWithI18n } from "../test/i18n";
import { MemberDetailPage } from "./member-detail-page";

const WS = "ws-test";
const surfaceProps = vi.hoisted(() => ({ current: null as Record<string, unknown> | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    settings: () => "/acme/settings",
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (select: (state: unknown) => unknown) =>
    select({ advancedFilter: null, clearFilters: vi.fn() }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../issues/components/filter-chips-bar", () => ({
  useFilterChips: () => ({ chips: [] }),
}));
vi.mock("../issues/components/issues-header", () => ({
  IssueFilterMenu: ({ trigger }: { trigger: ReactNode }) => <>{trigger}</>,
  IssueDisplayControls: () => <button type="button">Display</button>,
  ViewRefreshIndicator: () => null,
}));
vi.mock("../issues/surface/issue-surface", () => ({
  IssueSurface: (props: {
    renderHeader: (context: { controller: unknown; issues: unknown[] }) => ReactNode;
  }) => {
    surfaceProps.current = props;
    return (
      <div data-testid="issue-surface">
        {props.renderHeader({
          controller: {
            surfaceIssues: [],
            isRefreshing: false,
            facetCountsExact: true,
            setActiveTableFacet: vi.fn(),
          },
          issues: [],
        })}
      </div>
    );
  },
}));

const MEMBER: MemberWithUser = {
  id: "m-1",
  workspace_id: WS,
  user_id: "user-1",
  role: "admin",
  created_at: "2026-03-14T00:00:00Z",
  name: "Ada Lovelace",
  email: "ada@example.com",
  avatar_url: null,
};

function renderPage(userId = "user-1", members: MemberWithUser[] | null = [MEMBER]) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  if (members) qc.setQueryData(memberListOptions(WS).queryKey, members);
  qc.setQueryData(agentListOptions(WS).queryKey, [
    { id: "agent-1", name: "Babbage", owner_id: "user-1", archived_at: null },
    { id: "agent-2", name: "Retired", owner_id: "user-1", archived_at: "2026-01-01" },
  ] as Agent[]);
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <MemberDetailPage userId={userId} />
    </QueryClientProvider>,
  );
}

describe("MemberDetailPage", () => {
  beforeEach(() => {
    surfaceProps.current = null;
    actorIssuesViewStore.getState().setScope("assigned");
  });

  it("puts the breadcrumb and the issue tools in one header above a full-width list", () => {
    renderPage();

    expect(screen.getAllByRole("banner")).toHaveLength(1);
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Members" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=members",
    );
    expect(header).toHaveTextContent("Ada Lovelace");
    expect(within(header).queryByRole("button", { name: "Search issues..." })).not.toBeInTheDocument();
    expect(within(header).getByRole("button", { name: "Filter" })).toBeInTheDocument();
    expect(within(header).getByRole("button", { name: "Display" })).toBeInTheDocument();
    expect(within(header).queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
    expect(screen.getByTestId("issue-surface")).toBeInTheDocument();
    expect(surfaceProps.current?.scope).toEqual({
      type: "actor",
      actorType: "member",
      actorId: "user-1",
      relation: "assigned",
    });
  });

  it("shows who the member is in the properties panel", () => {
    renderPage();

    expect(screen.getByRole("button", { name: "Properties" })).toBeInTheDocument();
    expect(screen.getByText("Role")).toBeInTheDocument();
    expect(screen.getByText("Admin")).toBeInTheDocument();
    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    expect(screen.getByText("Joined")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Babbage" })).toHaveAttribute(
      "href",
      "/acme/agents/agent-1",
    );
    expect(screen.queryByText("Retired")).not.toBeInTheDocument();
  });

  it("counts the Created relation as a filter and resets it", () => {
    actorIssuesViewStore.getState().setScope("created");
    renderPage();

    const header = screen.getByRole("banner");
    expect(within(header).getByRole("button", { name: "1 filter" })).toBeInTheDocument();
    fireEvent.click(within(header).getByRole("button", { name: "Reset" }));
    expect(actorIssuesViewStore.getState().scope).toBe("assigned");
  });

  it("shows not-found in the body under the Members breadcrumb", () => {
    renderPage("missing");

    expect(
      within(screen.getByRole("banner")).getByRole("link", { name: "Members" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Member not found" })).toBeInTheDocument();
    expect(screen.queryByTestId("issue-surface")).not.toBeInTheDocument();
  });
});
