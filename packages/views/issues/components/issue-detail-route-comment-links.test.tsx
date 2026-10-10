import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { NavigationProvider } from "../../navigation";
import { IssueDetail } from "./issue-detail";
import { IssueDetailRoute } from "./issue-detail-route";

vi.mock("./issue-detail", async () => {
  const actual = await vi.importActual<typeof import("./issue-detail")>("./issue-detail");
  return { ...actual, IssueDetail: vi.fn(() => null) };
});

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

const replace = vi.fn();
const push = vi.fn();

describe("IssueDetailRoute comment deep links", () => {
  const issueId = "cb240efb-154c-42a8-ae92-42b02676feca";
  let qc: QueryClient;

  beforeEach(() => {
    replace.mockClear();
    push.mockClear();
    vi.mocked(IssueDetail).mockClear();
    // Electron's browser location need not match the active tab's location.
    window.history.replaceState(null, "", "/#comment-browser-comment");
    setApiInstance({
      getIssue: vi.fn().mockResolvedValue({ id: issueId, identifier: "TRS-134" }),
    } as unknown as ApiClient);
    qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    });
  });

  afterEach(() => {
    qc.clear();
    window.history.replaceState(null, "", "/");
  });

  function route(hash: string, routeId = "TRS-134") {
    return (
      <QueryClientProvider client={qc}>
        <NavigationProvider
          value={{
            push,
            replace,
            back: vi.fn(),
            pathname: `/acme/issues/${routeId}`,
            searchParams: new URLSearchParams(),
            hash,
            getShareableUrl: (p: string) => `https://app.multica.com${p}`,
          }}
        >
          <IssueDetailRoute routeId={routeId} />
        </NavigationProvider>
      </QueryClientProvider>
    );
  }

  function expectHighlight(commentId: string | undefined) {
    expect(vi.mocked(IssueDetail).mock.lastCall?.[0]).toMatchObject({
      issueId,
      highlightCommentId: commentId,
    });
  }

  it("highlights the adapter comment instead of the browser fragment", async () => {
    render(route("#comment-adapter-comment"));
    await waitFor(() => expectHighlight("adapter-comment"));
    expect(replace).not.toHaveBeenCalled();
  });

  it("retains the adapter fragment when canonicalizing a UUID route", async () => {
    render(route("#comment-adapter-comment", issueId));
    await waitFor(() => {
      expectHighlight("adapter-comment");
      expect(replace).toHaveBeenCalledWith("/acme/issues/TRS-134#comment-adapter-comment");
    });
    expect(push).not.toHaveBeenCalled();
  });

  it("updates the highlight when only the adapter hash changes", async () => {
    const { rerender } = render(route("#comment-first"));
    await waitFor(() => expect(IssueDetail).toHaveBeenCalled());
    rerender(route("#comment-second"));
    expectHighlight("second");
  });

  it.each(["", "#activity"])("clears the highlight for adapter hash %j", async (hash) => {
    const { rerender } = render(route("#comment-first"));
    await waitFor(() => expect(IssueDetail).toHaveBeenCalled());
    rerender(route(hash));
    expectHighlight(undefined);
  });
});
