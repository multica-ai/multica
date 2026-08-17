/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Issue } from "../types";
import { createQueryClient } from "../query-client";
import { issueKeys } from "./queries";
import { isIssueUuid, useCanonicalIssue } from "./canonical-id";

const ISSUE_UUID = "cb240efb-154c-42a8-ae92-42b02676feca";

const issue = {
  id: ISSUE_UUID,
  workspace_id: "ws-1",
  number: 134,
  identifier: "TRS-134",
  title: "Example",
  status: "todo",
  priority: "medium",
} as Issue;

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("isIssueUuid", () => {
  it("distinguishes a UUID from an issue identifier", () => {
    expect(isIssueUuid(ISSUE_UUID)).toBe(true);
    expect(isIssueUuid(ISSUE_UUID.toUpperCase())).toBe(true);
    expect(isIssueUuid("TRS-134")).toBe(false);
    expect(isIssueUuid("trs-134")).toBe(false);
    expect(isIssueUuid("")).toBe(false);
  });
});

describe("useCanonicalIssue", () => {
  let qc: QueryClient;
  let getIssue: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    // The app's real client, NOT a bare `new QueryClient()`. Its
    // `staleTime: Infinity` is load-bearing here: under the library default of
    // 0 every cache entry is stale the instant it lands, so a second observer
    // background-refetches and a request-count assertion measures the harness
    // rather than the code.
    qc = createQueryClient();
    getIssue = vi.fn(async () => issue);
    setApiInstance({ getIssue } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  // A UUID segment needs no resolution step, so the only request is the detail
  // read the issue view needs anyway — never an extra one.
  it("passes a UUID through without a resolution request", async () => {
    const { result } = renderHook(() => useCanonicalIssue("ws-1", ISSUE_UUID), {
      wrapper: createWrapper(qc),
    });

    expect(result.current.canonicalId).toBe(ISSUE_UUID);
    expect(result.current.isResolving).toBe(false);

    await waitFor(() => expect(result.current.issue).toEqual(issue));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(getIssue).toHaveBeenCalledTimes(1);
    expect(getIssue).toHaveBeenCalledWith(ISSUE_UUID);
  });

  it("resolves an identifier to the issue UUID", async () => {
    const { result } = renderHook(() => useCanonicalIssue("ws-1", "TRS-134"), {
      wrapper: createWrapper(qc),
    });

    expect(result.current.isResolving).toBe(true);
    await waitFor(() => expect(result.current.canonicalId).toBe(ISSUE_UUID));
    expect(result.current.isResolving).toBe(false);
    expect(result.current.issue).toEqual(issue);
    expect(getIssue).toHaveBeenCalledWith("TRS-134");
  });

  // The whole point of `initialData` over a post-resolve cache seed: the
  // canonical query must never observe an empty cache and start its own fetch.
  // A seed from an effect runs after that decision.
  // The common path: an in-app link carries the UUID, the route rewrites the
  // address bar to the identifier, and that rewrite re-renders this hook with
  // the identifier as its segment. Without the identifier-keyed seed the
  // resolution query missed and re-fetched an issue already in hand, so one
  // issue open cost two requests.
  it("costs no extra request when the URL is rewritten to the identifier", async () => {
    const { result, rerender } = renderHook(
      ({ routeId }: { routeId: string }) => useCanonicalIssue("ws-1", routeId),
      { wrapper: createWrapper(qc), initialProps: { routeId: ISSUE_UUID } },
    );

    await waitFor(() => expect(result.current.issue).toEqual(issue));
    expect(getIssue).toHaveBeenCalledTimes(1);

    rerender({ routeId: "TRS-134" });

    await waitFor(() => expect(result.current.canonicalId).toBe(ISSUE_UUID));
    expect(result.current.issue).toEqual(issue);
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(getIssue).toHaveBeenCalledTimes(1);
  });

  it("opens an identifier URL with exactly one request", async () => {
    const { result } = renderHook(() => useCanonicalIssue("ws-1", "TRS-134"), {
      wrapper: createWrapper(qc),
    });

    await waitFor(() => expect(result.current.issue).toEqual(issue));
    // Let any follow-up fetch settle before counting.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(getIssue).toHaveBeenCalledTimes(1);
  });

  // The realtime updaters patch `issueKeys.detail(wsId, issue.id)` with the
  // UUID from the websocket payload; the resolution response is older and must
  // never overwrite it.
  it("does not clobber a realtime-patched entry", async () => {
    const patched = { ...issue, status: "done" } as Issue;
    qc.setQueryData(issueKeys.detail("ws-1", ISSUE_UUID), patched);

    const { result } = renderHook(() => useCanonicalIssue("ws-1", "TRS-134"), {
      wrapper: createWrapper(qc),
    });

    await waitFor(() => expect(result.current.canonicalId).toBe(ISSUE_UUID));
    expect(result.current.issue).toEqual(patched);
    expect(qc.getQueryData(issueKeys.detail("ws-1", ISSUE_UUID))).toEqual(patched);
  });

  it("reports a nonexistent identifier as terminally not-found", async () => {
    getIssue.mockRejectedValue(new Error("issue not found"));

    const { result } = renderHook(() => useCanonicalIssue("ws-1", "ZZZ-134"), {
      wrapper: createWrapper(qc),
    });

    // Generous timeout on purpose: the app's client retries once, so a
    // genuinely unresolvable identifier takes a retry round-trip to settle.
    await waitFor(() => expect(result.current.notFound).toBe(true), { timeout: 5000 });
    expect(result.current.canonicalId).toBeNull();
    expect(result.current.issue).toBeUndefined();
    // Must NOT read as still-resolving: a caller that keeps showing a loading
    // frame here re-enters the remount loop this state exists to prevent.
    expect(result.current.isResolving).toBe(false);
  });

  // Regression: a failed resolution used to present as "not resolving, no id",
  // so the route handed the raw identifier to IssueDetail, whose observer
  // refetched the failed query, flipped this hook back to resolving, unmounted
  // IssueDetail, and remounted it on the next failure — tens of thousands of
  // requests in under a second, with the UI stuck on the skeleton.
  it("settles a failed resolution without looping requests", async () => {
    const noRetry = new QueryClient({
      // Retry off isolates the loop from the app's `retry: 1`, so a count above
      // 1 can only mean a remount refetched.
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    });
    getIssue.mockRejectedValue(new Error("issue not found"));

    const { result, rerender } = renderHook(() => useCanonicalIssue("ws-1", "ZZZ-134"), {
      wrapper: createWrapper(noRetry),
    });

    await waitFor(() => expect(result.current.notFound).toBe(true));
    const settled = getIssue.mock.calls.length;
    expect(settled).toBe(1);

    rerender();
    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(getIssue).toHaveBeenCalledTimes(settled);
    expect(result.current.notFound).toBe(true);

    noRetry.clear();
  });

  it("stays idle without a workspace id", () => {
    const { result } = renderHook(() => useCanonicalIssue("", "TRS-134"), {
      wrapper: createWrapper(qc),
    });

    expect(result.current.canonicalId).toBeNull();
    expect(result.current.isResolving).toBe(false);
    expect(getIssue).not.toHaveBeenCalled();
  });
});
