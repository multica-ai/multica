/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { issueKeys } from "../issues/queries";
import type {
  Issue,
  IssueLabelsResponse,
  Label,
  ListLabelsResponse,
} from "../types";
import { useAttachLabel, useDetachLabel } from "./mutations";
import { labelKeys } from "./queries";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const labelA: Label = {
  id: "label-a",
  workspace_id: "ws-1",
  name: "bug",
  color: "#ef4444",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const labelB: Label = {
  id: "label-b",
  workspace_id: "ws-1",
  name: "feature",
  color: "#22c55e",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const issue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "MUL-1",
  title: "Child",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "member-1",
  parent_issue_id: "parent-1",
  project_id: null,
  position: 1,
  stage: null,
  start_date: null,
  due_date: null,
  labels: [labelA],
  metadata: {},
  properties: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function startStaleChildrenFetch(qc: QueryClient, labels: Label[]) {
  const childKey = issueKeys.children("ws-1", "parent-1");
  const response = deferred<Issue[]>();
  const fetch = qc
    .fetchQuery({
      queryKey: childKey,
      queryFn: () => response.promise,
    })
    .catch(() => undefined);
  return { childKey, response, fetch, staleIssue: { ...issue, labels } };
}

describe("issue label mutations", () => {
  afterEach(() => vi.restoreAllMocks());

  it("prevents an older children fetch from overwriting an optimistic attach", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const childKey = issueKeys.children("ws-1", "parent-1");
    qc.setQueryData<Issue[]>(childKey, [issue]);
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue("ws-1", issue.id), {
      labels: [labelA],
    });
    qc.setQueryData<ListLabelsResponse>(labelKeys.list("ws-1"), {
      labels: [labelA, labelB],
      total: 2,
    });
    const stale = startStaleChildrenFetch(qc, [labelA]);
    await waitFor(() =>
      expect(qc.getQueryState(childKey)?.fetchStatus).toBe("fetching"),
    );

    const write = deferred<IssueLabelsResponse>();
    setApiInstance({
      attachLabel: vi.fn(() => write.promise),
    } as unknown as ApiClient);
    const { result } = renderHook(() => useAttachLabel(issue.id), {
      wrapper: wrapper(qc),
    });

    act(() => result.current.mutate(labelB.id));
    await waitFor(() =>
      expect(qc.getQueryData<Issue[]>(childKey)?.[0]?.labels).toEqual([
        labelA,
        labelB,
      ]),
    );

    stale.response.resolve([stale.staleIssue]);
    await stale.fetch;
    expect(qc.getQueryData<Issue[]>(childKey)?.[0]?.labels).toEqual([
      labelA,
      labelB,
    ]);

    await act(async () => {
      write.resolve({ labels: [labelA, labelB] });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    qc.clear();
  });

  it("prevents an older children fetch from overwriting an optimistic detach", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const childKey = issueKeys.children("ws-1", "parent-1");
    const labeledIssue = { ...issue, labels: [labelA, labelB] };
    qc.setQueryData<Issue[]>(childKey, [labeledIssue]);
    qc.setQueryData<IssueLabelsResponse>(labelKeys.byIssue("ws-1", issue.id), {
      labels: [labelA, labelB],
    });
    const stale = startStaleChildrenFetch(qc, [labelA, labelB]);
    await waitFor(() =>
      expect(qc.getQueryState(childKey)?.fetchStatus).toBe("fetching"),
    );

    const write = deferred<IssueLabelsResponse>();
    setApiInstance({
      detachLabel: vi.fn(() => write.promise),
    } as unknown as ApiClient);
    const { result } = renderHook(() => useDetachLabel(issue.id), {
      wrapper: wrapper(qc),
    });

    act(() => result.current.mutate(labelB.id));
    await waitFor(() =>
      expect(qc.getQueryData<Issue[]>(childKey)?.[0]?.labels).toEqual([labelA]),
    );

    stale.response.resolve([stale.staleIssue]);
    await stale.fetch;
    expect(qc.getQueryData<Issue[]>(childKey)?.[0]?.labels).toEqual([labelA]);

    await act(async () => {
      write.resolve({ labels: [labelA] });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    qc.clear();
  });
});
