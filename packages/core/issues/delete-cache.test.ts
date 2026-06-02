import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it } from "vitest";

import { cleanupDeletedIssueCaches } from "./delete-cache";
import { issueKeys } from "./queries";
import { useRecentIssuesStore } from "./stores/recent-issues-store";

const WS_ID = "ws-a";

beforeEach(() => {
  useRecentIssuesStore.setState({ byWorkspace: {} });
});

describe("cleanupDeletedIssueCaches — recent issues store", () => {
  it("removes the deleted issue from the recent issues bucket", () => {
    const { recordVisit } = useRecentIssuesStore.getState();
    recordVisit(WS_ID, "issue-1");
    recordVisit(WS_ID, "issue-2");

    const qc = new QueryClient();
    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");

    const ids = useRecentIssuesStore
      .getState()
      .byWorkspace[WS_ID]?.map((e) => e.id);
    expect(ids).toEqual(["issue-2"]);
  });

  it("does not touch the recent bucket of an unrelated workspace", () => {
    const { recordVisit } = useRecentIssuesStore.getState();
    recordVisit(WS_ID, "issue-1");
    recordVisit("ws-b", "issue-1");

    const qc = new QueryClient();
    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");

    const state = useRecentIssuesStore.getState().byWorkspace;
    expect(state[WS_ID]).toBeUndefined();
    expect(state["ws-b"]?.map((e) => e.id)).toEqual(["issue-1"]);
  });

  it("still removes the cached detail query for the deleted issue", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail(WS_ID, "issue-1"), { id: "issue-1" });

    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");

    expect(qc.getQueryData(issueKeys.detail(WS_ID, "issue-1"))).toBeUndefined();
  });
});
