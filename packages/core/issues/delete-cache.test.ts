import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it } from "vitest";

import { useRecentContextStore } from "../chat/recent-context-store";
import type { Issue } from "../types";
import {
  cleanupDeletedIssueCaches,
  collectDeletedIssueCacheMetadata,
} from "./delete-cache";
import { issueKeys } from "./queries";
import { useRecentIssuesStore } from "./stores/recent-issues-store";

const WS_ID = "ws-a";
const IDENTIFIER = "MUL-1";

function issue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: WS_ID,
    number: 1,
    identifier: IDENTIFIER,
    title: "Issue 1",
    parent_issue_id: null,
    ...overrides,
  } as Issue;
}

beforeEach(() => {
  useRecentIssuesStore.setState({ byWorkspace: {} });
  useRecentContextStore.setState({ byWorkspace: {} });
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

// Both stores are localStorage-backed, and only one of them used to be cleared
// here — the other was cleared by the delete mutation itself, so a delete that
// arrived over the websocket (another member, another window) left the issue
// sitting in this client's chat `@` context picker until gc.
describe("cleanupDeletedIssueCaches — recent context store", () => {
  it("forgets the deleted issue's chat context entry", () => {
    const { recordVisit } = useRecentContextStore.getState();
    recordVisit(WS_ID, { type: "issue", id: "issue-1", label: IDENTIFIER });
    recordVisit(WS_ID, { type: "issue", id: "issue-2" });

    cleanupDeletedIssueCaches(new QueryClient(), WS_ID, "issue-1");

    const ids = useRecentContextStore
      .getState()
      .byWorkspace[WS_ID]?.map((e) => `${e.type}:${e.id}`);
    expect(ids).toEqual(["issue:issue-2"]);
  });

  it("leaves a project entry that happens to share the issue's id", () => {
    const { recordVisit } = useRecentContextStore.getState();
    recordVisit(WS_ID, { type: "project", id: "issue-1" });

    cleanupDeletedIssueCaches(new QueryClient(), WS_ID, "issue-1");

    const ids = useRecentContextStore
      .getState()
      .byWorkspace[WS_ID]?.map((e) => `${e.type}:${e.id}`);
    expect(ids).toEqual(["project:issue-1"]);
  });
});

// Two caches key on the identifier rather than the UUID: the `MUL-1` autolink
// resolver, and the detail alias `useCanonicalIssue` mirrors so an identifier
// URL resolves without a second request. Dropping only the UUID entry left a
// deleted issue rendering as a live chip for the rest of the session.
describe("cleanupDeletedIssueCaches — identifier-keyed entries", () => {
  it("collects the identifier from the detail cache", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail(WS_ID, "issue-1"), issue());

    expect(collectDeletedIssueCacheMetadata(qc, WS_ID, "issue-1").identifier)
      .toBe(IDENTIFIER);
  });

  it("collects the identifier from a list cache when no detail is loaded", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.list(WS_ID), {
      byStatus: { todo: { issues: [issue()], total: 1 } },
    });

    expect(collectDeletedIssueCacheMetadata(qc, WS_ID, "issue-1").identifier)
      .toBe(IDENTIFIER);
  });

  it("removes the identifier resolver and the identifier detail alias", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail(WS_ID, "issue-1"), issue());
    qc.setQueryData(issueKeys.detail(WS_ID, IDENTIFIER), issue());
    qc.setQueryData(issueKeys.identifier(WS_ID, IDENTIFIER), issue());

    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");

    expect(qc.getQueryData(issueKeys.detail(WS_ID, IDENTIFIER))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.identifier(WS_ID, IDENTIFIER))).toBeUndefined();
  });

  it("leaves another issue's identifier entries alone", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail(WS_ID, "issue-1"), issue());
    qc.setQueryData(issueKeys.identifier(WS_ID, "MUL-2"), issue({ id: "issue-2", identifier: "MUL-2" }));

    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");

    expect(qc.getQueryData(issueKeys.identifier(WS_ID, "MUL-2"))).toBeDefined();
  });

  // The autolink resolver caches the whole Issue, so it can be the only cache
  // that knows the issue exists: a page whose single reference to it is a
  // `MUL-1` chip in a comment, deleted from another client (the WS event carries
  // only the UUID). Nothing else can supply the identifier here.
  it("finds the identifier from the resolver cache alone", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.identifier(WS_ID, IDENTIFIER), issue());

    expect(collectDeletedIssueCacheMetadata(qc, WS_ID, "issue-1").identifier)
      .toBe(IDENTIFIER);

    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");
    expect(qc.getQueryData(issueKeys.identifier(WS_ID, IDENTIFIER))).toBeUndefined();
  });

  // Same for the identifier-keyed detail alias on its own.
  it("finds the identifier from the detail alias alone", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail(WS_ID, IDENTIFIER), issue());

    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");
    expect(qc.getQueryData(issueKeys.detail(WS_ID, IDENTIFIER))).toBeUndefined();
  });

  it("ignores a resolver entry that resolved to a different issue", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.detail(WS_ID, "issue-1"), issue());
    qc.setQueryData(
      issueKeys.identifier(WS_ID, "MUL-9"),
      issue({ id: "issue-9", identifier: "MUL-9" }),
    );

    cleanupDeletedIssueCaches(qc, WS_ID, "issue-1");

    expect(qc.getQueryData(issueKeys.identifier(WS_ID, "MUL-9"))).toBeDefined();
  });

  it("cleans up without an identifier when no cache held the row at all", () => {
    const qc = new QueryClient();

    expect(collectDeletedIssueCacheMetadata(qc, WS_ID, "issue-1").identifier).toBeNull();
    expect(() => cleanupDeletedIssueCaches(qc, WS_ID, "issue-1")).not.toThrow();
  });

  // A resolver entry that answered "no such issue" is a null payload; it must
  // not be mistaken for the deleted row.
  it("tolerates a null resolver payload", () => {
    const qc = new QueryClient();
    qc.setQueryData(issueKeys.identifier(WS_ID, "MUL-404"), null);
    qc.setQueryData(issueKeys.detail(WS_ID, "issue-1"), issue());

    expect(() => cleanupDeletedIssueCaches(qc, WS_ID, "issue-1")).not.toThrow();
  });
});
