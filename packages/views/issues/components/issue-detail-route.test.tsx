// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, renderHook, waitFor, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Issue } from "@multica/core/types";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useIssueDetailSplitStore } from "@multica/core/issues/stores";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import { IssueDetailRoute, useCanonicalIssueUrl } from "./issue-detail-route";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

// Desktop / compact layout switch for the two-column detail route.
const layout = vi.hoisted(() => ({ compact: false }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.compact,
  useIsCompact: () => layout.compact,
}));

// The rail's own list query is stubbed; everything else in the queries module
// (issueDetailOptions, issueKeys, ...) stays real so `useCanonicalIssue` still
// resolves through the actual API client.
const railIssues = vi.hoisted(() => [] as unknown[]);
vi.mock("@multica/core/issues/queries", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/issues/queries")>(
    "@multica/core/issues/queries",
  );
  return {
    ...actual,
    issueListOptions: () => ({
      queryKey: ["issues", "ws-1", "list-sorted"],
      queryFn: () => Promise.resolve(railIssues),
    }),
  };
});

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ResizableHandle: () => null,
}));

vi.mock("./issue-detail", () => ({
  IssueDetail: () => <div data-testid="issue-detail" />,
  IssueDetailSkeleton: () => <div data-testid="issue-detail-skeleton" />,
  IssueNotFound: () => <div data-testid="issue-detail-not-found" />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

const replace = vi.fn();
const push = vi.fn();

function wrapper({ children }: { children: ReactNode }) {
  const adapter: NavigationAdapter = {
    push,
    replace,
    back: vi.fn(),
    pathname: "/acme/issues/x",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  };
  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

describe("useCanonicalIssueUrl", () => {
  beforeEach(() => {
    replace.mockClear();
    push.mockClear();
  });

  it("rewrites a UUID URL to the identifier once the issue resolves", () => {
    const { rerender } = renderHook(
      ({ identifier }: { identifier?: string }) =>
        useCanonicalIssueUrl("cb240efb-154c-42a8-ae92-42b02676feca", identifier),
      { wrapper, initialProps: {} },
    );

    // Nothing to rewrite to while the issue is still loading.
    expect(replace).not.toHaveBeenCalled();

    rerender({ identifier: "TRS-134" });
    expect(replace).toHaveBeenCalledWith("/acme/issues/TRS-134");
    expect(push).not.toHaveBeenCalled();
  });

  it("leaves an already-canonical URL alone", () => {
    renderHook(() => useCanonicalIssueUrl("TRS-134", "TRS-134"), { wrapper });
    expect(replace).not.toHaveBeenCalled();
  });

  // `useWorkspacePaths()` returns a fresh object per call, so the effect's
  // dependencies change identity on every commit. Without the ref guard this
  // re-fired the replace forever.
  it("rewrites once, not on every render", () => {
    const { rerender } = renderHook(
      () => useCanonicalIssueUrl("cb240efb-154c-42a8-ae92-42b02676feca", "TRS-134"),
      { wrapper },
    );

    rerender();
    rerender();
    expect(replace).toHaveBeenCalledTimes(1);
  });

  // A lowercase key resolves server-side, so the URL must still be normalized
  // to the issue's real identifier rather than left as typed.
  it("normalizes a differently-cased identifier segment", () => {
    renderHook(() => useCanonicalIssueUrl("trs-134", "TRS-134"), { wrapper });
    expect(replace).toHaveBeenCalledWith("/acme/issues/TRS-134");
  });
});

describe("IssueDetailRoute with an identifier that names no issue", () => {
  // Regression: the route used to fall through to IssueDetail with the raw
  // identifier. IssueDetail mounted a second observer on the query that had
  // just failed, `retryOnMount` refetched it, the route flipped back to its
  // skeleton and unmounted IssueDetail, then remounted it when the refetch
  // failed — an unbounded request loop that never reached "not found".
  // Retry is off so any count above 1 can only be a remount refetch.
  it("settles on not-found without looping requests", async () => {
    replace.mockClear();
    push.mockClear();
    const getIssue = vi.fn().mockRejectedValue(new Error("issue not found"));
    setApiInstance({ getIssue } as unknown as ApiClient);
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    });

    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <NavigationProvider
          value={{
            push,
            replace,
            back: vi.fn(),
            pathname: "/acme/issues/ZZZ-134",
            searchParams: new URLSearchParams(),
            getShareableUrl: (p: string) => `https://app.multica.com${p}`,
          }}
        >
          <IssueDetailRoute routeId="ZZZ-134" />
        </NavigationProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => expect(getIssue).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 250));
    expect(getIssue).toHaveBeenCalledTimes(1);

    rerender(
      <QueryClientProvider client={qc}>
        <NavigationProvider
          value={{
            push,
            replace,
            back: vi.fn(),
            pathname: "/acme/issues/ZZZ-134",
            searchParams: new URLSearchParams(),
            getShareableUrl: (p: string) => `https://app.multica.com${p}`,
          }}
        >
          <IssueDetailRoute routeId="ZZZ-134" />
        </NavigationProvider>
      </QueryClientProvider>,
    );
    await new Promise((resolve) => setTimeout(resolve, 250));
    expect(getIssue).toHaveBeenCalledTimes(1);

    // A failed resolve must never rewrite the URL.
    expect(replace).not.toHaveBeenCalled();
    qc.clear();
  });
});

function makeIssue(id: string, identifier: string, title: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier,
    title,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
  };
}

const issueA = makeIssue("issue-1", "MUL-1", "First issue");
const issueB = makeIssue("issue-2", "MUL-2", "Second issue");

function renderRoute(routeId: string, qc: QueryClient) {
  const adapter: NavigationAdapter = {
    push,
    replace,
    back: vi.fn(),
    pathname: `/acme/issues/${routeId}`,
    searchParams: new URLSearchParams(),
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  };
  return render(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <IssueDetailRoute routeId={routeId} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

describe("IssueDetailRoute desktop split layout", () => {
  beforeEach(() => {
    replace.mockClear();
    push.mockClear();
    layout.compact = false;
    useIssueDetailSplitStore.setState({ collapsed: false });
    railIssues.splice(0, railIssues.length, issueA, issueB);
  });

  function newClient() {
    return new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    });
  }

  it("renders a two-column desktop layout: left list rail + right detail", async () => {
    setApiInstance({ getIssue: vi.fn().mockResolvedValue(issueA) } as unknown as ApiClient);
    const qc = newClient();
    renderRoute("MUL-1", qc);

    expect(await screen.findByTestId("issue-detail")).toBeInTheDocument();
    expect(screen.getByTestId("issue-detail-rail-list")).toBeInTheDocument();
    expect(
      await screen.findByTestId("issue-detail-rail-row-issue-2"),
    ).toBeInTheDocument();
    qc.clear();
  });

  it("keeps the full-width detail without the left rail on compact widths", async () => {
    layout.compact = true;
    setApiInstance({ getIssue: vi.fn().mockResolvedValue(issueA) } as unknown as ApiClient);
    const qc = newClient();
    renderRoute("MUL-1", qc);

    expect(await screen.findByTestId("issue-detail")).toBeInTheDocument();
    expect(screen.queryByTestId("issue-detail-rail-list")).not.toBeInTheDocument();
    expect(screen.queryByTestId("issue-detail-rail-collapsed")).not.toBeInTheDocument();
    qc.clear();
  });

  it("navigates in place when a rail row is clicked", async () => {
    setApiInstance({ getIssue: vi.fn().mockResolvedValue(issueA) } as unknown as ApiClient);
    const qc = newClient();
    renderRoute("MUL-1", qc);

    fireEvent.click(await screen.findByTestId("issue-detail-rail-row-issue-2"));

    expect(replace).toHaveBeenCalledWith("/acme/issues/issue-2");
    qc.clear();
  });

  it("collapses and expands the rail via the header toggle, writing the store", async () => {
    setApiInstance({ getIssue: vi.fn().mockResolvedValue(issueA) } as unknown as ApiClient);
    const qc = newClient();
    renderRoute("MUL-1", qc);

    const toggle = await screen.findByTestId("issue-detail-rail-toggle");
    fireEvent.click(toggle);

    expect(useIssueDetailSplitStore.getState().collapsed).toBe(true);
    expect(screen.getByTestId("issue-detail-rail-collapsed")).toBeInTheDocument();
    expect(screen.queryByTestId("issue-detail-rail-list")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("issue-detail-rail-toggle"));

    expect(useIssueDetailSplitStore.getState().collapsed).toBe(false);
    expect(screen.getByTestId("issue-detail-rail-list")).toBeInTheDocument();
    expect(screen.queryByTestId("issue-detail-rail-collapsed")).not.toBeInTheDocument();
    qc.clear();
  });
});
