import { QueryClient } from "@tanstack/react-query";
import type { Issue, IssueReaction, TimelineEntry } from "@multica/core/types";
import { describe, expect, it, vi } from "vitest";

import { issueKeys } from "@/data/queries/issue-keys";
import {
  addCommentReaction,
  addIssueReaction,
  onIssueAuxiliaryRevision,
  invalidateIssueAfterReconnect,
  patchIssueDetail,
  patchIssueLabels,
  removeIssueReaction,
  replaceCommentTimelineEntry,
} from "./issue-ws-updaters";

describe("invalidateIssueAfterReconnect", () => {
  it("invalidates attachments together with the issue and task caches", () => {
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const wsId = "workspace-1";
    const issueId = "issue-1";

    invalidateIssueAfterReconnect(qc, wsId, issueId);

    expect(invalidate.mock.calls.map(([filters]) => filters?.queryKey)).toEqual([
      issueKeys.detail(wsId, issueId),
      issueKeys.timeline(wsId, issueId),
      issueKeys.attachments(wsId, issueId),
      issueKeys.activeTasks(wsId, issueId),
      issueKeys.tasks(wsId, issueId),
    ]);
  });
});

describe("mobile issue revision gates", () => {
  const wsId = "workspace-1";
  const issueId = "issue-1";

  it("rejects stale issue snapshots and accepts a newer one", () => {
    const qc = new QueryClient();
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), {
      id: issueId,
      title: "revision 5",
      revision: 5,
    } as Issue);

    patchIssueDetail(qc, wsId, { id: issueId, title: "stale", revision: 4 });
    expect(qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId))?.title).toBe("revision 5");

    patchIssueDetail(qc, wsId, { id: issueId, title: "revision 6", revision: 6 });
    expect(qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId))).toMatchObject({
      title: "revision 6",
      revision: 6,
    });
  });

  it("invalidates instead of blind-merging an unversioned event over versioned state", () => {
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), {
      id: issueId,
      title: "known",
      revision: 5,
    } as Issue);

    patchIssueDetail(qc, wsId, { id: issueId, title: "unknown order" });

    expect(qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId))?.title).toBe("known");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issueKeys.detail(wsId, issueId) });
  });

  it("keeps comment content and reactions monotonic", () => {
    const qc = new QueryClient();
    const key = issueKeys.timeline(wsId, issueId);
    const current = {
      type: "comment",
      id: "comment-1",
      actor_type: "member",
      actor_id: "member-1",
      created_at: "2026-01-01T00:00:00Z",
      content: "revision 5",
      revision: 5,
      reactions: [],
    } as TimelineEntry;
    qc.setQueryData<TimelineEntry[]>(key, [current]);

    replaceCommentTimelineEntry(qc, wsId, issueId, {
      ...current,
      content: "stale",
      revision: 4,
    });
    addCommentReaction(qc, wsId, issueId, "comment-1", {
      id: "reaction-1",
      comment_id: "comment-1",
      actor_type: "member",
      actor_id: "member-1",
      emoji: "👍",
      created_at: "2026-01-01T00:00:00Z",
    }, 4);
    expect(qc.getQueryData<TimelineEntry[]>(key)?.[0]).toMatchObject({
      content: "revision 5",
      revision: 5,
      reactions: [],
    });

    replaceCommentTimelineEntry(qc, wsId, issueId, {
      ...current,
      content: "revision 6",
      revision: 6,
    });
    expect(qc.getQueryData<TimelineEntry[]>(key)?.[0]).toMatchObject({
      content: "revision 6",
      revision: 6,
    });
  });

  it("invalidates after a partial owner event without replacing the full snapshot revision", () => {
    const qc = new QueryClient();
    qc.setQueryData<Issue>(issueKeys.detail(wsId, issueId), {
      id: issueId,
      revision: 2,
    } as Issue);
    onIssueAuxiliaryRevision(qc, wsId, issueId, 7);
    expect(qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId))?.revision).toBe(2);
    expect(qc.getQueryState(issueKeys.detail(wsId, issueId))?.isInvalidated).toBe(true);
  });

  it("accepts a full r2 snapshot after a revision-only r3 event", () => {
    const qc = new QueryClient();
    const key = issueKeys.detail(wsId, issueId);
    qc.setQueryData<Issue>(key, {
      id: issueId,
      title: "A",
      revision: 1,
    } as Issue);

    onIssueAuxiliaryRevision(qc, wsId, issueId, 3);
    patchIssueDetail(qc, wsId, { id: issueId, title: "B", revision: 2 });

    expect(qc.getQueryData<Issue>(key)).toMatchObject({
      title: "B",
      revision: 2,
    });
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
  });

  it("applies auxiliary labels without claiming they are a full issue snapshot", () => {
    const qc = new QueryClient();
    const key = issueKeys.detail(wsId, issueId);
    qc.setQueryData<Issue>(key, {
      id: issueId,
      labels: [],
      revision: 1,
    } as unknown as Issue);

    patchIssueLabels(qc, wsId, issueId, [{
      id: "label-1",
      workspace_id: wsId,
      name: "Latest",
      color: "#000000",
      created_at: "2026-08-17T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    }], 3);

    expect(qc.getQueryData<Issue>(key)).toMatchObject({
      revision: 1,
      labels: [{ id: "label-1" }],
    });
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
  });

  it("rejects a delayed labels event after a newer labels projection", () => {
    const qc = new QueryClient();
    const key = issueKeys.detail(wsId, issueId);
    qc.setQueryData<Issue>(key, {
      id: issueId,
      labels: [],
      revision: 1,
    } as unknown as Issue);

    patchIssueLabels(qc, wsId, issueId, [{
      id: "label-latest",
      workspace_id: wsId,
      name: "Latest",
      color: "#000000",
      created_at: "2026-08-18T00:00:00Z",
      updated_at: "2026-08-18T00:00:00Z",
    }], 3);
    patchIssueLabels(qc, wsId, issueId, [{
      id: "label-stale",
      workspace_id: wsId,
      name: "Stale",
      color: "#000000",
      created_at: "2026-08-17T00:00:00Z",
      updated_at: "2026-08-17T00:00:00Z",
    }], 2);

    expect(qc.getQueryData<Issue>(key)?.labels).toMatchObject([
      { id: "label-latest" },
    ]);
  });

  it("does not let a newer generic event block the first labels projection", () => {
    const qc = new QueryClient();
    const key = issueKeys.detail(wsId, issueId);
    qc.setQueryData<Issue>(key, {
      id: issueId,
      labels: [],
      revision: 1,
    } as unknown as Issue);

    onIssueAuxiliaryRevision(qc, wsId, issueId, 4);
    patchIssueLabels(
      qc,
      wsId,
      issueId,
      [
        {
          id: "label-3",
          workspace_id: wsId,
          name: "Independent",
          color: "#000000",
          created_at: "2026-08-18T00:00:00Z",
          updated_at: "2026-08-18T00:00:00Z",
        },
      ],
      3,
    );

    expect(qc.getQueryData<Issue>(key)?.labels).toMatchObject([
      { id: "label-3" },
    ]);
  });

  it("rejects a delayed issue-reaction add after a newer removal", () => {
    const qc = new QueryClient();
    const key = issueKeys.detail(wsId, issueId);
    const reaction: IssueReaction = {
      id: "reaction-1",
      issue_id: issueId,
      actor_type: "member",
      actor_id: "member-1",
      emoji: "👍",
      created_at: "2026-08-17T00:00:00Z",
    };
    qc.setQueryData<Issue>(key, {
      id: issueId,
      reactions: [reaction],
      revision: 1,
    } as unknown as Issue);

    removeIssueReaction(qc, wsId, issueId, reaction.emoji, reaction.actor_id, 3);
    addIssueReaction(qc, wsId, issueId, reaction, 2);

    expect(qc.getQueryData<Issue>(key)?.reactions).toEqual([]);
  });
});
