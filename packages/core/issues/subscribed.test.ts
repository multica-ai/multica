import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "./queries";
import { refreshSubscribedIssues, subscribedIssueKeys } from "./subscribed";

describe("refreshSubscribedIssues", () => {
  it("invalidates the id set and only the issue queries filtered by subscribed", async () => {
    const qc = new QueryClient();
    const subscribedRows = [...issueKeys.tableAll("ws-1"), "rows", { filters: { relations: ["subscribed"] } }];
    const subscribedTree = [
      ...issueKeys.tableAll("ws-1"),
      "facets",
      { filters: { advanced: { match: "all", children: [{ field: "relations", values: ["subscribed"] }] } } },
    ];
    const assignedRows = [...issueKeys.tableAll("ws-1"), "rows", { filters: { relations: ["assigned"] } }];
    const otherWorkspace = [...issueKeys.tableAll("ws-2"), "rows", { filters: { relations: ["subscribed"] } }];
    for (const key of [subscribedRows, subscribedTree, assignedRows, otherWorkspace]) qc.setQueryData(key, {});
    qc.setQueryData(subscribedIssueKeys.all("ws-1"), ["issue-1"]);

    await refreshSubscribedIssues(qc, "ws-1");

    const stale = (key: readonly unknown[]) => qc.getQueryState(key)?.isInvalidated;
    expect(stale(subscribedRows)).toBe(true);
    expect(stale(subscribedTree)).toBe(true);
    expect(stale(subscribedIssueKeys.all("ws-1"))).toBe(true);
    expect(stale(assignedRows)).toBe(false);
    expect(stale(otherWorkspace)).toBe(false);
  });
});
