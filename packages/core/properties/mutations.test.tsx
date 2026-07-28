/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { issueKeys } from "../issues/queries";
import { onIssuePropertiesChanged } from "../issues/ws-updaters";
import type { Issue, ListIssuesCache } from "../types";
import { useSetIssueProperty } from "./mutations";

const WS_ID = "workspace-1";
const ISSUE_ID = "issue-1";

function makeIssue(properties: Issue["properties"]): Issue {
  return {
    id: ISSUE_ID,
    workspace_id: WS_ID,
    number: 1,
    identifier: "MUL-1",
    kind: "issue",
    title: "Prioritize launch",
    description: null,
    status: "todo",
    priority: "high",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    metadata: {},
    properties,
    start_date: null,
    due_date: null,
    created_at: "2026-07-17T00:00:00Z",
    updated_at: "2026-07-17T00:00:00Z",
  };
}

function makeListCache(issue: Issue): ListIssuesCache {
  return { byStatus: { todo: { issues: [issue], total: 1 } } };
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("useSetIssueProperty", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically changes one key from a list-only cache without losing sibling values", async () => {
    const request = deferred<{ properties: Issue["properties"] }>();
    const setIssueProperty = vi.fn(() => request.promise);
    setApiInstance({ setIssueProperty } as unknown as ApiClient);
    const listKey = issueKeys.listSorted(WS_ID, { sort_by: "position" });
    qc.setQueryData<ListIssuesCache>(
      listKey,
      makeListCache(makeIssue({ "property-effort": 50000 })),
    );

    const { result } = renderHook(() => useSetIssueProperty(WS_ID), {
      wrapper: createWrapper(qc),
    });
    let pending!: Promise<unknown>;
    act(() => {
      pending = result.current.mutateAsync({
        issueId: ISSUE_ID,
        propertyId: "property-business-value",
        value: 125000,
      });
    });

    await waitFor(() => {
      const issue = qc.getQueryData<ListIssuesCache>(listKey)?.byStatus.todo?.issues[0];
      expect(issue?.properties).toEqual({
        "property-effort": 50000,
        "property-business-value": 125000,
      });
    });

    request.resolve({
      properties: {
        "property-effort": 50000,
        "property-business-value": 125000,
      },
    });
    await act(async () => pending);
  });

  it("rolls back only the failed key and preserves a concurrent realtime write", async () => {
    const request = deferred<{ properties: Issue["properties"] }>();
    const setIssueProperty = vi.fn(() => request.promise);
    setApiInstance({ setIssueProperty } as unknown as ApiClient);
    qc.setQueryData<Issue>(
      issueKeys.detail(WS_ID, ISSUE_ID),
      makeIssue({ "property-business-value": 100000 }),
    );

    const { result } = renderHook(() => useSetIssueProperty(WS_ID), {
      wrapper: createWrapper(qc),
    });
    let pending!: Promise<unknown>;
    act(() => {
      pending = result.current.mutateAsync({
        issueId: ISSUE_ID,
        propertyId: "property-business-value",
        value: 125000,
      });
    });

    await waitFor(() => {
      expect(
        qc.getQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID))?.properties,
      ).toEqual({ "property-business-value": 125000 });
    });

    onIssuePropertiesChanged(qc, WS_ID, ISSUE_ID, {
      "property-business-value": 125000,
      "property-effort": 50000,
    });
    request.reject(new Error("write failed"));
    await act(async () => {
      await expect(pending).rejects.toThrow("write failed");
    });

    expect(qc.getQueryData<Issue>(issueKeys.detail(WS_ID, ISSUE_ID))?.properties).toEqual({
      "property-business-value": 100000,
      "property-effort": 50000,
    });
  });

  it("does not refetch a flat property window before the mutation commits", async () => {
    const request = deferred<{ properties: Issue["properties"] }>();
    const setIssueProperty = vi.fn(() => request.promise);
    setApiInstance({ setIssueProperty } as unknown as ApiClient);
    const flatKey = issueKeys.flat(
      WS_ID,
      "workspace:all",
      {},
      {
        sort_by: "property:property-estimate",
        properties: { "property-estimate": ["2"] },
      },
    );
    qc.setQueryData(flatKey, {
      pages: [
        {
          issues: [makeIssue({ "property-estimate": 1 })],
          total: 1,
        },
      ],
      pageParams: [0],
    });

    const { result } = renderHook(() => useSetIssueProperty(WS_ID), {
      wrapper: createWrapper(qc),
    });
    let pending!: Promise<unknown>;
    act(() => {
      pending = result.current.mutateAsync({
        issueId: ISSUE_ID,
        propertyId: "property-estimate",
        value: 2,
      });
    });

    await waitFor(() => {
      const issue = qc.getQueryData<{ pages: { issues: Issue[] }[] }>(
        flatKey,
      )?.pages[0]?.issues[0];
      expect(issue?.properties["property-estimate"]).toBe(2);
    });
    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);

    request.resolve({ properties: { "property-estimate": 2 } });
    await act(async () => pending);

    await waitFor(() => {
      expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(true);
    });
  });
});
