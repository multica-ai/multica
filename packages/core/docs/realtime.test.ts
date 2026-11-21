import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type { Doc, DocTree } from "../types";
import { docKeys } from "./queries";
import { applyDocUpdated, holdLiveDoc } from "./realtime";

const WS = "ws-1";
const summary = (revision: number, title = "Plan") => ({
  id: "doc-1",
  workspace_id: WS,
  folder_id: null,
  title,
  revision,
  version_count: 2,
  created_by_type: "member",
  created_by_id: "u1",
  updated_by_type: "member",
  updated_by_id: "u2",
  created_at: "2026-11-21T00:00:00Z",
  updated_at: "2026-11-21T00:00:05Z",
});
const doc = (revision: number, content: string): Doc => ({
  id: "doc-1",
  workspaceId: WS,
  folderId: null,
  title: "Plan",
  revision,
  versionCount: 1,
  createdByType: "member",
  createdById: "u1",
  updatedByType: "member",
  updatedById: "u1",
  createdAt: "2026-11-21T00:00:00Z",
  updatedAt: "2026-11-21T00:00:00Z",
  content,
});

function setup() {
  const qc = new QueryClient();
  const { content: _content, ...listed } = doc(1, "");
  qc.setQueryData<DocTree>(docKeys.tree(WS), { folders: [], docs: [listed] });
  qc.setQueryData<Doc>(docKeys.detail(WS, "doc-1"), doc(1, "old body"));
  qc.setQueryData(docKeys.comments(WS, "doc-1"), []);
  qc.setQueryData(docKeys.versions(WS, "doc-1"), []);
  return qc;
}
const invalidated = (qc: QueryClient, key: readonly unknown[]) => qc.getQueryState(key)?.isInvalidated;

describe("applyDocUpdated", () => {
  it("patches a live doc in place without refetching its body or comments", () => {
    const qc = setup();
    const release = holdLiveDoc("doc-1");
    applyDocUpdated(qc, WS, { doc: summary(5, "Plan v2"), version: 2 });
    release();

    expect(qc.getQueryData<DocTree>(docKeys.tree(WS))!.docs[0]).toMatchObject({ title: "Plan v2", revision: 5 });
    expect(invalidated(qc, docKeys.tree(WS))).toBe(false);
    expect(qc.getQueryData<Doc>(docKeys.detail(WS, "doc-1"))).toMatchObject({ title: "Plan v2", revision: 5, versionCount: 2 });
    // Marked for the next open, but not fetched while the live session holds the body.
    expect(invalidated(qc, docKeys.detail(WS, "doc-1"))).toBe(true);
    expect(qc.getQueryState(docKeys.detail(WS, "doc-1"))!.fetchStatus).toBe("idle");
    expect(invalidated(qc, docKeys.comments(WS, "doc-1"))).toBe(false);
    expect(invalidated(qc, docKeys.versions(WS, "doc-1"))).toBe(true);
  });

  it("refetches the body of a doc that is not edited live", () => {
    const qc = setup();
    applyDocUpdated(qc, WS, { doc: summary(5) });
    expect(invalidated(qc, docKeys.detail(WS, "doc-1"))).toBe(true);
    expect(qc.getQueryData<Doc>(docKeys.detail(WS, "doc-1"))!.revision).toBe(1);
  });

  it("applies a metadata change at the same revision, such as a move", () => {
    const qc = setup();
    applyDocUpdated(qc, WS, { doc: { ...summary(1), folder_id: "f1" } });
    expect(qc.getQueryData<Doc>(docKeys.detail(WS, "doc-1"))).toMatchObject({ folderId: "f1", content: "old body" });
    expect(invalidated(qc, docKeys.detail(WS, "doc-1"))).toBe(false);
    expect(qc.getQueryData<DocTree>(docKeys.tree(WS))!.docs[0]!.folderId).toBe("f1");
    expect(invalidated(qc, docKeys.versions(WS, "doc-1"))).toBe(false);
  });

  it("ignores an event older than the cache", () => {
    const qc = setup();
    qc.setQueryData<Doc>(docKeys.detail(WS, "doc-1"), doc(7, "newer"));
    applyDocUpdated(qc, WS, { doc: summary(5, "stale") });
    expect(qc.getQueryData<Doc>(docKeys.detail(WS, "doc-1"))).toMatchObject({ revision: 7, title: "Plan" });
    expect(invalidated(qc, docKeys.detail(WS, "doc-1"))).toBe(false);
  });

  it("falls back to refetching everything on a payload it cannot read", () => {
    const qc = setup();
    applyDocUpdated(qc, WS, { doc: { id: "doc-1" } });
    expect(invalidated(qc, docKeys.tree(WS))).toBe(true);
    expect(invalidated(qc, docKeys.comments(WS, "doc-1"))).toBe(true);
  });
});
