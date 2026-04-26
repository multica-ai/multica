import { describe, it, expect } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { linkKeys } from "./queries";
import { issueKeys } from "../issues/queries";
import type { Issue, IssueLink } from "../types";

const wsId = "ws-1";
const issueId = "issue-a";

function makeLink(id: string, overrides: Partial<IssueLink> = {}): IssueLink {
  return {
    id,
    pair_id: "pair-" + id,
    link_type: "blocks",
    direction: "outgoing",
    creator_type: "member",
    creator_id: "u-1",
    created_at: "2026-01-01T00:00:00Z",
    target_issue_id: "issue-b",
    target_identifier: "TIM-2",
    target_title: "Target",
    target_status: "todo",
    target_number: 2,
    target_workspace_id: wsId,
    target_workspace_name: "Workspace",
    target_workspace_slug: "ws",
    ...overrides,
  };
}

function makeIssue(links: IssueLink[]): Issue {
  // Cast through unknown — only the `id`/`workspace_id`/`links` fields are
  // exercised by the cache transform, so we don't need to enumerate every
  // Issue field for a unit test of the links transform.
  return {
    id: issueId,
    workspace_id: wsId,
    links,
  } as unknown as Issue;
}

// ---------------------------------------------------------------------------
// linkKeys factory
// ---------------------------------------------------------------------------

describe("linkKeys", () => {
  it("scopes the root key under the workspace id", () => {
    expect(linkKeys.all(wsId)).toEqual(["links", wsId]);
  });

  it("derives forIssue from the root key", () => {
    expect(linkKeys.forIssue(wsId, issueId)).toEqual([
      "links",
      wsId,
      "issue",
      issueId,
    ]);
  });

  it("derives blockersForIssue from the root key", () => {
    expect(linkKeys.blockersForIssue(wsId, issueId)).toEqual([
      "links",
      wsId,
      "blockers",
      issueId,
    ]);
  });

  it("forIssue and blockersForIssue do not collide", () => {
    expect(linkKeys.forIssue(wsId, issueId)).not.toEqual(
      linkKeys.blockersForIssue(wsId, issueId),
    );
  });

  it("different workspace ids produce different keys", () => {
    expect(linkKeys.all("ws-1")).not.toEqual(linkKeys.all("ws-2"));
    expect(linkKeys.forIssue("ws-1", issueId)).not.toEqual(
      linkKeys.forIssue("ws-2", issueId),
    );
  });

  it("invalidating the root key matches descendant keys", () => {
    // TanStack matches keys by prefix; verify the hierarchical relationship.
    const root = linkKeys.all(wsId);
    const issueKey = linkKeys.forIssue(wsId, issueId);
    const blockersKey = linkKeys.blockersForIssue(wsId, issueId);
    expect(issueKey.slice(0, root.length)).toEqual(root);
    expect(blockersKey.slice(0, root.length)).toEqual(root);
  });
});

// ---------------------------------------------------------------------------
// Cache transform behavior — mirrors the optimistic-id filter applied in
// onSuccess of useCreateIssueLink. The transform itself is module-private,
// so we replay the same operation against a QueryClient to confirm the
// invariants the rest of the system depends on.
// ---------------------------------------------------------------------------

describe("issue.links cache transform", () => {
  it("addressing the issue cache via issueKeys.detail round-trips a transform", () => {
    const qc = new QueryClient();
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), makeIssue([]));

    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
      old ? { ...old, links: [...(old.links ?? []), makeLink("real-1")] } : old,
    );

    const after = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
    expect(after?.links?.map((l) => l.id)).toEqual(["real-1"]);
  });

  it("optimistic-id filter drops 'optimistic-*' rows and keeps real ones", () => {
    const qc = new QueryClient();
    qc.setQueryData<Issue>(
      issueKeys.detail(wsId, issueId),
      makeIssue([makeLink("optimistic-1"), makeLink("real-1")]),
    );

    // Replay the create-link onSuccess transform: drop optimistic placeholders,
    // append the server row.
    const serverRow = makeLink("real-2");
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) => {
      if (!old) return old;
      const withoutOptimistic = (old.links ?? []).filter(
        (l) => !l.id.startsWith("optimistic-"),
      );
      return { ...old, links: [...withoutOptimistic, serverRow] };
    });

    const after = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
    expect(after?.links?.map((l) => l.id)).toEqual(["real-1", "real-2"]);
  });

  it("delete-link transform removes the matching link by id", () => {
    const qc = new QueryClient();
    qc.setQueryData<Issue>(
      issueKeys.detail(wsId, issueId),
      makeIssue([makeLink("link-1"), makeLink("link-2"), makeLink("link-3")]),
    );

    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
      old
        ? { ...old, links: (old.links ?? []).filter((l) => l.id !== "link-2") }
        : old,
    );

    const after = qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId));
    expect(after?.links?.map((l) => l.id)).toEqual(["link-1", "link-3"]);
  });

  it("transform on a cold cache (no entry yet) is a no-op and does not seed the cache", () => {
    const qc = new QueryClient();
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), (old) =>
      old ? { ...old, links: [makeLink("real-1")] } : old,
    );
    expect(
      qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId)),
    ).toBeUndefined();
  });
});
