/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Issue, IssuePropertiesResponse } from "../types";
import { issueKeys } from "../issues/queries";
import { useSetIssueProperty } from "./mutations";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const issue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "MUL-1",
  title: "Estimate",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "member-1",
  parent_issue_id: null,
  project_id: null,
  position: 1,
  stage: null,
  start_date: null,
  due_date: null,
  labels: [],
  metadata: {},
  properties: { estimate: 1 },
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useSetIssueProperty", () => {
  afterEach(() => vi.restoreAllMocks());

  it("does not refetch a property window before the mutation commits", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const flatKey = issueKeys.flat(
      "ws-1",
      "workspace:all",
      {},
      { sort_by: "property:estimate", properties: { estimate: ["2"] } },
    );
    qc.setQueryData(flatKey, {
      pages: [{ issues: [issue], total: 1 }],
      pageParams: [0],
    });

    let resolveWrite!: (value: IssuePropertiesResponse) => void;
    const setIssueProperty = vi.fn(
      () =>
        new Promise<IssuePropertiesResponse>((resolve) => {
          resolveWrite = resolve;
        }),
    );
    setApiInstance({ setIssueProperty } as unknown as ApiClient);

    const { result } = renderHook(() => useSetIssueProperty(), {
      wrapper: wrapper(qc),
    });
    act(() => {
      result.current.mutate({
        issueId: issue.id,
        propertyId: "estimate",
        value: 2,
      });
    });

    await waitFor(() =>
      expect(
        qc.getQueryData<{ pages: { issues: Issue[] }[] }>(flatKey)?.pages[0]
          ?.issues[0]?.properties.estimate,
      ).toBe(2),
    );
    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);

    await act(async () => {
      resolveWrite({ properties: { estimate: 2 } });
    });
    await waitFor(() =>
      expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(true),
    );
    qc.clear();
  });
});
