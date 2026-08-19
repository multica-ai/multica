import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type { Issue } from "../types";
import { issueKeys } from "./queries";
import {
  onIssueLabelsChanged,
  onIssueMetadataChanged,
  onIssuePropertiesChanged,
  onIssueUpdated,
  onIssueAuxiliaryRevision,
} from "./ws-updaters";

function issue(revision: number, title: string): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    identifier: "MUL-1",
    number: 1,
    title,
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
    created_at: "2026-08-16T00:00:00Z",
    updated_at: "2026-08-16T00:00:00Z",
    revision,
    metadata: {},
    properties: {},
  };
}

describe("issue realtime revision admission", () => {
  it("heals an older projection without replacing an equal-revision detail", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail("ws-1", "issue-1"), issue(3, "latest"));
    qc.setQueryData(issueKeys.children("ws-1", "parent-1"), [
      issue(2, "stale child"),
    ]);

    onIssueUpdated(qc, "ws-1", issue(3, "latest"));

    expect(
      qc.getQueryData<Issue>(issueKeys.detail("ws-1", "issue-1"))?.title,
    ).toBe("latest");
    expect(
      qc.getQueryData<Issue[]>(issueKeys.children("ws-1", "parent-1"))?.[0],
    ).toMatchObject({ revision: 3, title: "latest" });
  });

  it("rejects an event older than the freshest cached revision", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail("ws-1", "issue-1"), issue(3, "latest"));
    qc.setQueryData(issueKeys.children("ws-1", "parent-1"), [
      issue(3, "latest"),
    ]);

    onIssueUpdated(qc, "ws-1", issue(2, "stale event"));

    expect(
      qc.getQueryData<Issue>(issueKeys.detail("ws-1", "issue-1"))?.title,
    ).toBe("latest");
    expect(
      qc.getQueryData<Issue[]>(issueKeys.children("ws-1", "parent-1"))?.[0]
        ?.title,
    ).toBe("latest");
  });

  it("refetches rather than applying an unversioned event over versioned state", () => {
    const qc = new QueryClient();
    const detailKey = issueKeys.detail("ws-1", "issue-1");
    qc.setQueryData(detailKey, issue(3, "latest"));

    onIssueUpdated(qc, "ws-1", { ...issue(1, "unversioned"), revision: undefined });

    expect(qc.getQueryData<Issue>(detailKey)?.title).toBe("latest");
    expect(qc.getQueryState(detailKey)?.isInvalidated).toBe(true);
  });

  it("invalidates stale owner projections without claiming a partial event is a full snapshot", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail("ws-1", "issue-1"), issue(5, "detail"));
    qc.setQueryData(issueKeys.children("ws-1", "parent-1"), [
      issue(3, "child"),
    ]);

    onIssueAuxiliaryRevision(qc, "ws-1", "issue-1", 4);

    expect(
      qc.getQueryData<Issue>(issueKeys.detail("ws-1", "issue-1"))?.revision,
    ).toBe(5);
    expect(
      qc.getQueryData<Issue[]>(issueKeys.children("ws-1", "parent-1"))?.[0]
        ?.revision,
    ).toBe(3);
    expect(
      qc.getQueryState(issueKeys.detail("ws-1", "issue-1"))?.isInvalidated,
    ).toBe(false);
    expect(
      qc.getQueryState(issueKeys.children("ws-1", "parent-1"))?.isInvalidated,
    ).toBe(true);
  });

  it("admits a fuller snapshot after a newer revision-only event", () => {
    const qc = new QueryClient();
    const detailKey = issueKeys.detail("ws-1", "issue-1");
    qc.setQueryData(detailKey, issue(1, "A"));

    onIssueAuxiliaryRevision(qc, "ws-1", "issue-1", 3);
    expect(qc.getQueryData<Issue>(detailKey)).toMatchObject({
      revision: 1,
      title: "A",
    });
    expect(qc.getQueryState(detailKey)?.isInvalidated).toBe(true);

    onIssueUpdated(qc, "ws-1", issue(2, "B"));
    expect(qc.getQueryData<Issue>(detailKey)).toMatchObject({
      revision: 2,
      title: "B",
    });
    expect(qc.getQueryState(detailKey)?.isInvalidated).toBe(true);
  });

  it("rejects stale auxiliary snapshots as a whole, not only their revision", () => {
    const qc = new QueryClient();
    const latestLabel = {
      id: "label-latest",
      workspace_id: "ws-1",
      name: "latest",
      color: "#000000",
      created_at: "2026-08-16T00:00:00Z",
      updated_at: "2026-08-16T00:00:00Z",
    };
    qc.setQueryData(issueKeys.detail("ws-1", "issue-1"), {
      ...issue(3, "latest"),
      metadata: { state: "latest" },
      properties: { state: "latest" },
      labels: [latestLabel],
    });

    onIssueMetadataChanged(qc, "ws-1", "issue-1", { state: "stale" }, 2);
    onIssuePropertiesChanged(qc, "ws-1", "issue-1", { state: "stale" }, 2);
    onIssueLabelsChanged(qc, "ws-1", "issue-1", [{
      ...latestLabel,
      id: "label-stale",
      name: "stale",
    }], 2);

    expect(qc.getQueryData<Issue>(issueKeys.detail("ws-1", "issue-1"))).toMatchObject({
      revision: 3,
      metadata: { state: "latest" },
      properties: { state: "latest" },
      labels: [{ id: "label-latest" }],
    });
  });

  it("does not apply an unversioned auxiliary snapshot over versioned state", () => {
    const qc = new QueryClient();
    const detailKey = issueKeys.detail("ws-1", "issue-1");
    qc.setQueryData(detailKey, {
      ...issue(3, "latest"),
      metadata: { state: "latest" },
    });

    onIssueMetadataChanged(qc, "ws-1", "issue-1", { state: "unversioned" });

    expect(qc.getQueryData<Issue>(detailKey)?.metadata).toEqual({ state: "latest" });
    expect(qc.getQueryState(detailKey)?.isInvalidated).toBe(true);
  });

  it("orders each auxiliary projection independently", () => {
    const qc = new QueryClient();
    const detailKey = issueKeys.detail("ws-1", "issue-1");
    qc.setQueryData(detailKey, {
      ...issue(1, "base"),
      metadata: { state: "base" },
      properties: { estimate: 1 },
    });

    onIssueMetadataChanged(qc, "ws-1", "issue-1", { state: "latest" }, 3);
    onIssueMetadataChanged(qc, "ws-1", "issue-1", { state: "stale" }, 2);
    onIssuePropertiesChanged(qc, "ws-1", "issue-1", { estimate: 2 }, 2);

    expect(qc.getQueryData<Issue>(detailKey)).toMatchObject({
      revision: 1,
      metadata: { state: "latest" },
      properties: { estimate: 2 },
    });
  });

  it("uses an equal-revision auxiliary event to heal only older projections", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail("ws-1", "issue-1"), {
      ...issue(3, "detail"),
      metadata: { state: "latest" },
    });
    qc.setQueryData(issueKeys.children("ws-1", "parent-1"), [{
      ...issue(2, "child"),
      metadata: { state: "stale" },
    }]);

    onIssueMetadataChanged(qc, "ws-1", "issue-1", { state: "latest" }, 3);

    expect(
      qc.getQueryData<Issue>(issueKeys.detail("ws-1", "issue-1"))?.title,
    ).toBe("detail");
    expect(
      qc.getQueryData<Issue[]>(issueKeys.children("ws-1", "parent-1"))?.[0],
    ).toMatchObject({ revision: 2, metadata: { state: "latest" } });
    expect(
      qc.getQueryState(issueKeys.children("ws-1", "parent-1"))?.isInvalidated,
    ).toBe(true);
  });
});

// MUL-6394: `getQueriesData` matches a key PREFIX, so a scan over `tableAll` /
// `myAll` also sees sibling caches with a different shape. Reading `rows` /
// `byStatus` off them threw "Cannot read properties of undefined (reading
// 'some')" out of `useCreateComment`'s onSuccess — the comment was created,
// the UI reported a failure.
describe("auxiliary revision — sibling caches under a shared key prefix", () => {
  const query = {
    scope: { kind: "workspace" },
    filters: {},
    sort: { field: "position", direction: "asc" },
  } as const;

  const rowPageKey = [
    ...issueKeys.tableRows(
      "ws-1",
      query,
      { kind: "none" },
      null,
      false,
      null,
    ),
    "page",
    null,
  ] as const;

  function seedSiblingCaches(qc: QueryClient) {
    // Grouped rows are an infinite cache, facets a plain object; neither has
    // a `rows` array, and both sit under the `table-query` prefix.
    qc.setQueryData(issueKeys.tableGroups("ws-1", query, { kind: "status" }), {
      pages: [
        {
          query_fingerprint: "sha256:groups",
          total: 1,
          groups: [],
          next_cursor: null,
        },
      ],
      pageParams: [null],
    });
    qc.setQueryData(
      issueKeys.tableFacets("ws-1", { query, facets: [{ kind: "status" }] }),
      { query_fingerprint: "sha256:facets", total: 1, facets: [] },
    );
    // The assignee-grouped caches share the `my` prefix with the bucketed
    // ones but carry no `byStatus`.
    qc.setQueryData(issueKeys.myAssigneeGroups("ws-1", "assigned", {}), {
      groups: [],
    });
  }

  it("skips them instead of throwing, and still invalidates stale row pages", () => {
    const qc = new QueryClient();
    seedSiblingCaches(qc);
    qc.setQueryData(rowPageKey, {
      query_fingerprint: "sha256:rows",
      group_key: null,
      parent_id: null,
      total: 1,
      rows: [{ issue: issue(2, "stale row"), direct_child_count: 0 }],
      branch_total: 1,
      next_cursor: null,
    });

    expect(() =>
      onIssueAuxiliaryRevision(qc, "ws-1", "issue-1", 3),
    ).not.toThrow();

    expect(qc.getQueryState(rowPageKey)?.isInvalidated).toBe(true);
    expect(
      qc.getQueryState(
        issueKeys.tableGroups("ws-1", query, { kind: "status" }),
      )?.isInvalidated,
    ).toBe(false);
    expect(
      qc.getQueryState(
        issueKeys.myAssigneeGroups("ws-1", "assigned", {}),
      )?.isInvalidated,
    ).toBe(false);
  });

  it("leaves an up-to-date row page alone", () => {
    const qc = new QueryClient();
    seedSiblingCaches(qc);
    qc.setQueryData(rowPageKey, {
      query_fingerprint: "sha256:rows",
      group_key: null,
      parent_id: null,
      total: 1,
      rows: [{ issue: issue(3, "current row"), direct_child_count: 0 }],
      branch_total: 1,
      next_cursor: null,
    });

    onIssueAuxiliaryRevision(qc, "ws-1", "issue-1", 3);

    expect(qc.getQueryState(rowPageKey)?.isInvalidated).toBe(false);
  });
});
