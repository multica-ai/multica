import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, renderHook, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type {
  Issue,
  IssueEnvironmentsResponse,
  IssuePullRequestsResponse,
  IssueWakeup,
  ListGitHubInstallationsResponse,
  ListVCSConnectionsResponse,
} from "@multica/core/types";
import { githubInstallationsOptions, issuePullRequestsOptions } from "@multica/core/github";
import { vcsConnectionsOptions } from "@multica/core/vcs/queries";
import { issueDetailOptions, issueDuplicatesOptions, issueEnvironmentsOptions } from "@multica/core/issues/queries";
import { issueWakeupsOptions } from "@multica/core/issues";
import { useIssueSidebarSectionsStore } from "@multica/core/issues/stores";
import { renderWithI18n } from "../../test/i18n";
import { IssueSidebarAddRow, useIssueSidebarAddables } from "./issue-sidebar-add-row";

let repos: unknown[] = [{ url: "https://github.com/acme/app" }];
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ({ id: "ws-1", repos }),
}));

vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("../utils/status-options", () => ({ useIssueStatusOptions: () => ({ options: [], workflow: null }) }));

const declared = { environments: [], configs: [{ environments: [{ name: "web" }] }], pending_starts: [], failed_starts: [] };
const noPullRequests = { pull_requests: [], auto_complete: { state: "none", pull_request_ids: [], issue_disabled: false, workspace_enabled: true } };

const unrelated = { id: "issue-1", status: "todo", blocked_count: 0, blocked_by: [], blocking: [] } as unknown as Issue;

function seed(overrides: { wakeups?: unknown; prs?: unknown; environments?: unknown; vcsConnections?: unknown[]; issue?: Partial<Issue> } = {}) {
  // Seeded and never stale, so the hook reads exactly what each section would.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(issueWakeupsOptions("ws-1", "issue-1").queryKey, (overrides.wakeups ?? []) as IssueWakeup[]);
  client.setQueryData(issueDetailOptions("ws-1", "issue-1").queryKey, { ...unrelated, ...overrides.issue } as Issue);
  client.setQueryData(issueDuplicatesOptions("ws-1", "issue-1").queryKey, { duplicate_of: null, duplicates: [] });
  client.setQueryData(issuePullRequestsOptions("issue-1").queryKey, (overrides.prs ?? noPullRequests) as IssuePullRequestsResponse);
  client.setQueryData(githubInstallationsOptions("ws-1").queryKey, { installations: [] } as unknown as ListGitHubInstallationsResponse);
  client.setQueryData(
    vcsConnectionsOptions("ws-1").queryKey,
    { connections: overrides.vcsConnections ?? [] } as unknown as ListVCSConnectionsResponse,
  );
  client.setQueryData(
    issueEnvironmentsOptions("ws-1", "issue-1").queryKey,
    (overrides.environments ?? declared) as IssueEnvironmentsResponse,
  );
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function addables(
  props: Partial<Parameters<typeof useIssueSidebarAddables>[0]> = {},
  data?: Parameters<typeof seed>[0],
) {
  return renderHook(
    () => useIssueSidebarAddables({ issueId: "issue-1", closed: false, enabled: true, pullRequestsAvailable: true, ...props }),
    { wrapper: seed(data) },
  ).result.current;
}

beforeEach(() => {
  repos = [{ url: "https://github.com/acme/app" }];
  useIssueSidebarSectionsStore.setState({ hidden: [] });
});

describe("useIssueSidebarAddables", () => {
  it("offers every empty section the reader can start", () => {
    expect(addables()).toEqual(["wakeups", "relations", "pull_requests", "environments"]);
  });

  it("offers relations only while the Relations section has nothing to list", () => {
    const blocker = { id: "b-1", identifier: "TEST-1", title: "Prerequisite", status: "todo", via: "manual" };
    expect(addables({}, { issue: { blocked_count: 1, blocked_by: [{ ...blocker, closed: false }] } })).not.toContain("relations");
    expect(addables({}, { issue: { blocking: [{ ...blocker, closed: false }] } })).not.toContain("relations");
    // A finished blocker is not listed, so relations are offered again.
    expect(addables({}, { issue: { blocked_by: [{ ...blocker, closed: true }] } })).toContain("relations");
  });

  it("drops sections that already have content", () => {
    expect(addables({}, { wakeups: [{ id: "wake" }], prs: { ...noPullRequests, pull_requests: [{ id: "pr" }] } })).toEqual(["relations", "environments"]);
  });

  it("offers nothing to a reader who cannot add, such as a guest", () => {
    expect(addables({ enabled: false })).toEqual([]);
  });

  it("leaves out sections hidden in the sidebar settings", () => {
    useIssueSidebarSectionsStore.setState({ hidden: ["pull_requests", "relations"] });
    expect(addables()).toEqual(["wakeups", "environments"]);
  });

  it("offers no wakeup on an ended issue", () => {
    expect(addables({ closed: true })).toEqual(["relations", "pull_requests", "environments"]);
  });

  it("offers pull requests only where the workspace has code and the sidebar shows them", () => {
    expect(addables({ pullRequestsAvailable: false })).toEqual(["wakeups", "relations", "environments"]);
    repos = [];
    expect(addables()).toEqual(["wakeups", "relations", "environments"]);
  });

  it("offers pull requests to a workspace that only connects a self-hosted VCS", () => {
    repos = [];
    expect(addables({}, { vcsConnections: [{ id: "gitlab-1" }] })).toEqual(["wakeups", "relations", "pull_requests", "environments"]);
  });

  it("offers an environment only when a run declared one and nothing is starting", () => {
    expect(addables({}, { environments: { ...declared, configs: [] } })).toEqual(["wakeups", "relations", "pull_requests"]);
    expect(addables({}, { environments: { ...declared, pending_starts: [{ id: "cmd" }] } })).toEqual(["wakeups", "relations", "pull_requests"]);
  });
});

describe("IssueSidebarAddRow", () => {
  it("renders nothing with nothing to add", () => {
    const { container } = renderWithI18n(<IssueSidebarAddRow items={[]} onPick={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("opens a lone section directly", () => {
    const onPick = vi.fn();
    renderWithI18n(<IssueSidebarAddRow items={["pull_requests"]} onPick={onPick} />);
    fireEvent.click(screen.getByRole("button", { name: "Add pull request" }));
    expect(onPick).toHaveBeenCalledWith("pull_requests");
  });

  it("lets the reader pick among several", async () => {
    const onPick = vi.fn();
    renderWithI18n(<IssueSidebarAddRow items={["wakeups", "environments"]} onPick={onPick} />, { locale: "zh-Hans" });
    fireEvent.click(screen.getByRole("button", { name: "添加唤醒或环境" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "环境" }));
    expect(onPick).toHaveBeenCalledWith("environments");
  });

  it("names two items and leaves more to the menu", () => {
    const { unmount } = renderWithI18n(<IssueSidebarAddRow items={["wakeups", "pull_requests"]} onPick={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Add wakeup or pull request" })).toBeInTheDocument();
    unmount();
    renderWithI18n(<IssueSidebarAddRow items={["wakeups", "relations", "pull_requests"]} onPick={vi.fn()} issue={unrelated} />);
    expect(screen.getByRole("button", { name: "Add..." })).toBeInTheDocument();
  });

  it("opens the relation types for relations alone", async () => {
    renderWithI18n(
      <QueryClientProvider client={new QueryClient()}>
        <IssueSidebarAddRow items={["relations"]} onPick={vi.fn()} issue={unrelated} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Add relation" }));
    expect(await screen.findByRole("menuitem", { name: /Wait for another task/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /Block another task/ })).toBeInTheDocument();
  });

  it("leaves relations out without an issue to relate", () => {
    renderWithI18n(<IssueSidebarAddRow items={["wakeups", "relations"]} onPick={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Add wakeup" })).toBeInTheDocument();
  });
});
